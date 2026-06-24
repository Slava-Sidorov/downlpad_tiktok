module download_tiktok

go 1.26

require (
	github.com/schollz/progressbar/v3 v3.0.0
	github.com/urfave/cli/v2 v2.0.0
)

replace github.com/schollz/progressbar/v3 => ./third_party/progressbar-v3

replace github.com/urfave/cli/v2 => ./third_party/urfave-cli-v2
