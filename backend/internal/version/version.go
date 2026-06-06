package version

var (
	Version = "1.0.8"
	Repo    = "MengMengCode/CLICD"
)

func Current() string {
	if Version == "" {
		return "dev"
	}
	return Version
}



