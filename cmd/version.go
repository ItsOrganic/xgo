package cmd

// Version is the build version, overridden at link time by GoReleaser via
// -X github.com/ItsOrganic/whack/cmd.Version=<tag>. It stays "dev" for
// builds made straight from source, which is how `go build` output should
// identify itself.
var Version = "dev"
