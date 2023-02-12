package sdk

import (
	// embed the commit.txt file into the binary.
	_ "embed"
)

//go:generate bash -c "bash ../../hack/git_commit.sh > commit.txt"
var (
	//go:embed commit.txt
	GitCommit string

	// Version of the program
	Version = "1.0.13" //nolint:gochecknoglobals
)
