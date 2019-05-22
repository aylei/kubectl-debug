package version

var (
	gitVersion = "v0.0.0-master+$Format:%h$"
)

func Version() string {
	return gitVersion
}
