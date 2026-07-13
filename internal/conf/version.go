package conf

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
	Author    = "SuperJeason"
	// Repo 为源码/发行仓库首页。在线更新默认据此推导 GitHub owner/repo，
	// 也可用环境变量 OCTOPUS_UPDATE_REPO 覆盖（格式 owner/repo）。
	Repo = "https://github.com/SuperJeason/octopus"
)
