package version

var (
	gitVersion = "v1.0.0-master+$Format:%h$"
)

func Version() string {
	return gitVersion
}
