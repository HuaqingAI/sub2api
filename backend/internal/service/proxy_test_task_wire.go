package service

func ProvideProxyTestTaskService(repo ProxyTestTaskRepository, admin AdminService, email *EmailService) *ProxyTestTaskService {
	ConfigureProxyTaskCoordination(admin, repo)
	return NewProxyTestTaskService(repo, admin, email)
}

func ProvideProxyTestTaskRunnerService(tasks *ProxyTestTaskService) *ProxyTestTaskRunnerService {
	runner := NewProxyTestTaskRunnerService(tasks)
	runner.Start()
	return runner
}
