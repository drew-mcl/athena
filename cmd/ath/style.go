package main

import "github.com/drewfead/athena/internal/cli"

// Style variables - use ANSI codes directly for CLI tool
// (The terminal detection in cli.Style() can fail at package init time)
var (
	reset     = "\033[0m"
	bold      = "\033[1m"
	dim       = "\033[2m"
	italic    = "\033[3m"
	underline = "\033[4m"

	black   = "\033[30m"
	red     = "\033[31m"
	green   = "\033[32m"
	yellow  = "\033[33m"
	blue    = "\033[34m"
	magenta = "\033[35m"
	cyan    = "\033[36m"
	white   = "\033[37m"
	gray    = "\033[90m"
)

const (
	boxTopLeft     = cli.BoxTopLeft
	boxTopRight    = cli.BoxTopRight
	boxBottomLeft  = cli.BoxBottomLeft
	boxBottomRight = cli.BoxBottomRight
	boxHorizontal  = cli.BoxHorizontal
	boxVertical    = cli.BoxVertical
	boxTeeRight    = cli.BoxTeeRight
	boxTeeLeft     = cli.BoxTeeLeft

	treeBranch     = cli.TreeBranch
	treeLastBranch = cli.TreeLastBranch
	treeVertical   = cli.TreeVertical
	treeSpace      = cli.TreeSpace

	checkMark    = cli.CheckMark
	bullet       = cli.Bullet
	circle       = cli.Circle
	arrowDown    = cli.ArrowDown
	arrowUp      = cli.ArrowUp
	questionMark = cli.QuestionMark

	shapeGoal          = cli.ShapeGoalOpen
	shapeFeature       = cli.ShapeFeatureOpen
	shapeTask          = cli.ShapeTaskOpen
	shapeGoalFilled    = cli.ShapeGoalFilled
	shapeFeatureFilled = cli.ShapeFeatureFilled
	shapeTaskFilled    = cli.ShapeTaskFilled
)
