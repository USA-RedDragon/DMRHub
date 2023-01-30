package sdk

import _ "embed"

//go:generate bash -c "../../hack/git_commit.sh > commit.txt"
var (
	// GitCommit that was compiled. This will be filled in by the compiler.
	//go:embed commit.txt
	GitCommit string

	// Version of the program
	Version = "0.1.0"
)
