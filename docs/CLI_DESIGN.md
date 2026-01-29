# CLI Output Design Guidelines

Design standards for all Athena CLI output. Follow these guidelines to maintain a consistent, beautiful, and readable terminal experience.

## Philosophy

1. **Subtlety over loudness** - IDs are dimmed, content is prominent
2. **Color for meaning** - Colors indicate type/status, not decoration
3. **Standard ANSI only** - Use basic 16-color palette for compatibility
4. **Clean hierarchy** - Tree connectors show relationships clearly
5. **Summary footers** - Always show counts at the bottom

## ANSI Color Codes

### Styles
| Code | Name | Usage |
|------|------|-------|
| `\033[0m` | Reset | Clear all formatting |
| `\033[1m` | Bold | Titles, headers |
| `\033[2m` | Dim | IDs, secondary info |
| `\033[3m` | Italic | Emphasis (use sparingly) |

### Standard Colors (30-37)
| Code | Name | Usage |
|------|------|-------|
| `\033[31m` | Red | Errors |
| `\033[32m` | Green | Success, features, clean status |
| `\033[33m` | Yellow | Warnings, tasks, changes, tickets |
| `\033[34m` | Blue | Goals |
| `\033[35m` | Magenta | Special highlights |
| `\033[36m` | Cyan | Branches, URLs |
| `\033[37m` | White | Primary content text |

### Bright Colors (90-97)
| Code | Name | Usage |
|------|------|-------|
| `\033[90m` | Gray | Borders, muted text, connectors |
| `\033[97m` | Bright White | Emphasized content |

## Work Item Shapes

| Type | Pending | Active | Color |
|------|---------|--------|-------|
| Goal | □ | ■ | Blue |
| Feature | ◇ | ◆ | Green |
| Task | ○ | ● | Yellow |

Completed items use gray regardless of shape.

## Status Indicators

| Symbol | Meaning | Color |
|--------|---------|-------|
| ✓ | Clean/Done | Green |
| ● | Changes/Active | Yellow |
| ↓ | Behind | Yellow |
| ↑ | Ahead | Blue |
| ? | Untracked | Yellow |

## Box Drawing

```
┌─────────────┐  Corners: ┌ ┐ └ ┘
│             │  Sides: │ ─
├─────────────┤  Tees: ├ ┤
└─────────────┘
```

## Tree Connectors

```
├─ item       Branch (not last child)
└─ item       Last child
│             Vertical continuation
  (space)     No continuation
```

## Layout Patterns

### Boxed Table (worktrees)

```
┌─ Title ─────────────────────────────────────────────────────────┐
│ COLUMN1              COLUMN2                         STATUS     │
├─────────────────────────────────────────────────────────────────┤
│ white-name           cyan-branch                     ✓          │
│ white-name           cyan-branch                     ● changes  │
└─────────────────────────────────────────────────────────────────┘

N items (X clean, Y with changes)
```

- Title: Bold
- Column headers: Dim
- NAME column: White
- BRANCH column: Cyan
- STATUS: Green (clean) / Yellow (changes)
- Borders: Gray

### Tree View (work items)

```
□ dim-id  white-subject [gray-progress]
├─◇ dim-id  white-subject
│ └─○ dim-id  white-subject
└─◇ dim-id  white-subject yellow-ticket

N items (X pending, Y active, Z done)
```

- Connectors: Gray
- Shape: Type-colored (blue/green/yellow)
- ID: Dim (subtle, not distracting)
- Subject: White
- Ticket: Yellow
- Progress: Gray
- Completed items: All gray

### Simple List (goals, features, tasks)

```
Title:
  □ dim-id  white-subject [progress]
  ◇ dim-id  white-subject ● active
  ○ dim-id  gray-subject (completed)

N items (X pending, Y active, Z done)
```

### Status View

```
Active:
  ■ dim-id  white-subject
    Agent: yellow-agent-id

Ready (N):
  ○ dim-id  gray-subject
  ... and N more
```

## Implementation Checklist

When adding new CLI output:

- [ ] Use standard 16-color ANSI codes only
- [ ] IDs are always dim (secondary info)
- [ ] Content text is white (primary focus)
- [ ] Shapes colored by type (blue/green/yellow)
- [ ] Status indicators consistent (✓ ● ↓ ↑ ?)
- [ ] Summary footer with counts
- [ ] Test with light and dark terminals
- [ ] No truncation unless absolutely necessary

## Code Example

```go
const (
    reset = "\033[0m"
    bold  = "\033[1m"
    dim   = "\033[2m"

    green  = "\033[32m"
    yellow = "\033[33m"
    blue   = "\033[34m"
    cyan   = "\033[36m"
    white  = "\033[37m"
    gray   = "\033[90m"
)

// ID is dim, text is white
fmt.Printf("%s%s%s  %s%s%s\n",
    dim, id, reset,
    white, subject, reset)
```

## Performance

- Use goroutines for I/O operations (git status, file reads)
- Pre-allocate slices when size is known
- Batch database queries where possible
