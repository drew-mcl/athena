package cli

// Box drawing characters
const (
	BoxTopLeft     = "┌"
	BoxTopRight    = "┐"
	BoxBottomLeft  = "└"
	BoxBottomRight = "┘"
	BoxHorizontal  = "─"
	BoxVertical    = "│"
	BoxTeeRight    = "├"
	BoxTeeLeft     = "┤"
	BoxTeeDown     = "┬"
	BoxTeeUp       = "┴"
	BoxCross       = "┼"
)

// Tree drawing characters
const (
	TreeBranch     = "├─"
	TreeLastBranch = "└─"
	TreeVertical   = "│ "
	TreeSpace      = "  "
)

// Status indicators
const (
	CheckMark   = "✓"
	Bullet      = "●"
	Circle      = "○"
	ArrowDown   = "↓"
	ArrowUp     = "↑"
	QuestionMark = "?"
)

// Work item shapes
const (
	ShapeGoalOpen       = "□"
	ShapeGoalFilled     = "■"
	ShapeFeatureOpen    = "◇"
	ShapeFeatureFilled  = "◆"
	ShapeTaskOpen       = "○"
	ShapeTaskFilled     = "●"
)
