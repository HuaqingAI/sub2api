package repository

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const proxyLatencyKeyPrefix = "proxy:latency:"

func proxyLatencyKey(proxyID int64) string {
	return fmt.Sprintf("%s%d", proxyLatencyKeyPrefix, proxyID)
}

type proxyLatencyCache struct {
	rdb *redis.Client
}

func NewProxyLatencyCache(rdb *redis.Client) service.ProxyLatencyCache {
	return &proxyLatencyCache{rdb: rdb}
}

func (c *proxyLatencyCache) GetProxyLatencies(ctx context.Context, proxyIDs []int64) (map[int64]*service.ProxyLatencyInfo, error) {
	results := make(map[int64]*service.ProxyLatencyInfo)
	if len(proxyIDs) == 0 {
		return results, nil
	}

	keys := make([]string, 0, len(proxyIDs))
	for _, id := range proxyIDs {
		keys = append(keys, proxyLatencyKey(id))
	}

	values, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return results, err
	}

	for i, raw := range values {
		if raw == nil {
			continue
		}
		var payload []byte
		switch v := raw.(type) {
		case string:
			payload = []byte(v)
		case []byte:
			payload = v
		default:
			continue
		}
		var info service.ProxyLatencyInfo
		if err := json.Unmarshal(payload, &info); err != nil {
			continue
		}
		results[proxyIDs[i]] = &info
	}

	return results, nil
}

func (c *proxyLatencyCache) SetProxyLatency(ctx context.Context, proxyID int64, info *service.ProxyLatencyInfo) error {
	if info == nil {
		return nil
	}
	payload, err := json.Marshal(info)
	if err != nil {
		return err
	}
	return setProxyLatencyScript.Run(ctx, c.rdb,
		[]string{proxyLatencyKey(proxyID), proxyLatencyKey(proxyID) + ":version"},
		payload, info.ProbeConfigVersion, info.SampleID, info.QualityOnly).Err()
}

// A sampling round is stored as one document. Lua makes the configuration fence,
// sample ordering and quality-only merge atomic across all application Pods.
var setProxyLatencyScript = redis.NewScript(`
local previousVersion = redis.call('GET', KEYS[2])
local version = ARGV[2]
if previousVersion and previousVersion ~= version and previousVersion > version then
    return 0
end
local incoming = cjson.decode(ARGV[1])
local previous = redis.call('GET', KEYS[1])
if previous then
    local ok, existing = pcall(cjson.decode, previous)
    if ok and (existing.probe_config_version or '') == version then
        if ARGV[4] == '1' then
            if existing.quality_checked_at and incoming.quality_checked_at and
               existing.quality_checked_at > incoming.quality_checked_at then return 0 end
            existing.quality_status = incoming.quality_status
            existing.quality_score = incoming.quality_score
            existing.quality_grade = incoming.quality_grade
            existing.quality_summary = incoming.quality_summary
            existing.quality_checked_at = incoming.quality_checked_at
            existing.quality_cf_ray = incoming.quality_cf_ray
            existing.ip_address = incoming.ip_address
            existing.country = incoming.country
            existing.country_code = incoming.country_code
            existing.region = incoming.region
            existing.city = incoming.city
            incoming = existing
        elseif (existing.sample_id or '') > ARGV[3] then return 0 end
        if existing.quality_checked_at and
           (not incoming.quality_checked_at or existing.quality_checked_at > incoming.quality_checked_at) then
            incoming.quality_status = existing.quality_status
            incoming.quality_score = existing.quality_score
            incoming.quality_grade = existing.quality_grade
            incoming.quality_summary = existing.quality_summary
            incoming.quality_checked_at = existing.quality_checked_at
            incoming.quality_cf_ray = existing.quality_cf_ray
        end
    end
end
redis.call('SET', KEYS[2], version)
redis.call('SET', KEYS[1], cjson.encode(incoming))
return 1
`)

func (c *proxyLatencyCache) InvalidateProxyLatency(ctx context.Context, proxyID int64, version string) error {
	return invalidateProxyLatencyScript.Run(ctx, c.rdb,
		[]string{proxyLatencyKey(proxyID), proxyLatencyKey(proxyID) + ":version"}, version).Err()
}

var invalidateProxyLatencyScript = redis.NewScript(`
local version = redis.call('GET', KEYS[2])
if version and version > ARGV[1] then return 0 end
redis.call('SET', KEYS[2], ARGV[1])
redis.call('DEL', KEYS[1])
return 1
`)

var _ service.ProxyLatencyInvalidator = (*proxyLatencyCache)(nil)
