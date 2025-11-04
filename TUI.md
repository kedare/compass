# Compass TUI Implementation Plan & Progress

## Overview
Implement a k9s-style keyboard-driven TUI interface for compass, accessible via `compass interactive`, providing full access to existing CLI features through an intuitive, navigable interface.

---

## ✅ Current Status: MVP Working

### What's Working Now (v0.1 - Minimal Viable Product)
- ✅ Basic TUI launches successfully via `compass interactive`
- ✅ Displays instances from cache in a table format
- ✅ Keyboard navigation (arrow keys, vim-style j/k)
- ✅ Clean display without log interference
- ✅ Proper Ctrl+C and 'q' quit handling
- ✅ Mouse support enabled
- ✅ Status bar with keyboard hints
- ✅ Loads cached instances from all projects

### Known Issues Fixed
- ❌ **Complex App architecture caused hang** - The original PageStack/Component lifecycle was causing a deadlock
- ✅ **Solution**: Implemented `RunDirect()` - a simplified direct runner that bypasses complex initialization
- ❌ **Logging interfered with display** - pterm debug output corrupted the terminal
- ✅ **Solution**: Removed logger dependency from TUI code

---

## Architecture Design

### Technology Stack
- **TUI Framework**: `tview` (github.com/rivo/tview v0.42.0)
- **Terminal Handling**: `tcell` (github.com/gdamore/tcell/v2 v2.9.0)
- **Pattern**: Simplified direct implementation (complex MVC abandoned due to deadlock)
- **Integration**: Reuses existing cache and GCP client

### Current Implementation Structure

```
internal/tui/
├── direct.go              # ✅ Working minimal TUI runner
├── app.go                 # ⚠️  Complex version (causes hang - not used)
├── app_simple.go          # ⚠️  Intermediate version (not used)
├── components.go          # ⚠️  Component interfaces (not used)
├── keys.go                # ⚠️  Key action system (not used)
├── styles.go              # ✅ Color scheme definitions (reusable)
├── page_stack.go          # ⚠️  Navigation stack (causes deadlock)
├── instance_view.go       # ⚠️  Complex instance view (not used)
├── help_view.go           # ⚠️  Help view (not used)
└── widgets/
    └── table.go           # ⚠️  Custom table widget (not used)

cmd/
└── interactive.go         # ✅ CLI subcommand using RunDirect()
```

---

## Complete Implementation Plan

### Phase 1: Foundation ✅ COMPLETED (MVP)
**Status**: Basic TUI working with simplified architecture

**What's Implemented**:
- [x] Add tview and tcell dependencies
- [x] Create minimal direct runner (`direct.go`)
- [x] CLI integration (`compass interactive` subcommand)
- [x] Load instances from cache
- [x] Table display with borders
- [x] Basic keyboard handling (Ctrl+C, q, arrows)
- [x] Mouse support
- [x] Status bar with hints

**Current Features**:
- Instance list table with columns: Name, Project, Zone, Status
- Navigation with arrow keys or vim-style (j/k - handled by tview)
- Status bar showing available shortcuts
- Clean exit with Ctrl+C or 'q'
- Loads all cached instances from all projects

---

### Phase 2: Enhanced Instance View 🚧 TODO
**Goal**: Add filtering, sorting, and SSH capability

**Features to Add**:
- [ ] Filter mode (press `/` to filter instances)
- [ ] Filter by name, project, zone, or IP
- [ ] Sort by column (press column number or `s` to cycle)
- [ ] Color-coded status (green=RUNNING, red=STOPPED, yellow=other)
- [ ] SSH to selected instance (press Enter)
  - [ ] Suspend TUI properly
  - [ ] Execute SSH via gcloud
  - [ ] Resume TUI after SSH exits
- [ ] Refresh instances (press `r`)
- [ ] Show instance details (press `d`)

**Implementation Notes**:
- Extend `direct.go` with filter input field (toggle visibility)
- Use `app.Suspend()` for SSH sessions
- Add color tags to status cells based on instance state
- Implement input field for filter with Enter/Esc handling

---

### Phase 3: Help System 📋 TODO
**Goal**: Provide keyboard shortcut reference

**Features to Add**:
- [ ] Help overlay (press `?`)
- [ ] Show all keyboard shortcuts
- [ ] Context-sensitive help per view
- [ ] Dismiss with Esc or ?

**Implementation**:
- Modal overlay with TextView
- Dynamic content based on current view
- Scrollable if content is long

---

### Phase 4: VPN Inspector 🔍 TODO
**Goal**: Browse VPN gateways, tunnels, and BGP sessions

**Features to Add**:
- [ ] Navigate to VPN view (press `v` from main view)
- [ ] Hierarchical tree view:
  ```
  ▼ project-1
    ▼ gateway-1 (2 tunnels, 2 BGP sessions)
      ▶ tunnel-1 (UP) [green]
      ▶ tunnel-2 (DOWN) [red] ⚠️
    ▶ gateway-2
  ▼ project-2
    ▶ gateway-3
  ```
- [ ] Expand/collapse with Enter or Space
- [ ] Detail panel showing gateway/tunnel/BGP info
- [ ] Color-coded status (UP=green, DOWN=red)
- [ ] Warning indicators for orphaned tunnels
- [ ] Refresh data (press `r`)

**Data Source**:
- Reuse `gcp.Client.ListVPNOverview()`
- Reuse `gcp.Client.GetVPNGateway()`
- Format using existing `output.DisplayVPNOverview()` logic

---

### Phase 5: Connectivity Tests 🔗 TODO
**Goal**: Manage network connectivity tests

**Features to Add**:
- [ ] Navigate to connectivity tests (press `c`)
- [ ] List all tests with status
- [ ] Create new test (press `n`)
  - [ ] Form-based test creation
  - [ ] Source/destination selection
  - [ ] Protocol/port configuration
- [ ] View test details (press Enter)
- [ ] Rerun test (press `r`)
- [ ] Delete test (press `d` or Del)
- [ ] Watch test until completion (press `w`)
- [ ] Color-coded results (REACHABLE=green, UNREACHABLE=red)

**Data Source**:
- Reuse `gcp.ConnectivityClient` methods
- Poll test status with backoff
- Display reachability analysis and path trace

---

### Phase 6: IP Lookup 🔎 TODO
**Goal**: Search for IP addresses across projects

**Features to Add**:
- [ ] Navigate to IP lookup (press `i`)
- [ ] Search input at top
- [ ] Results table below
- [ ] Incremental search (search as you type)
- [ ] Multi-project search with progress
- [ ] Navigate to resource (press Enter on result)
- [ ] Color-coded by resource type

**Data Source**:
- Reuse `gcp.Client.LookupIP()`
- Use subnet cache for fast lookups
- Show progress during multi-project scan

---

### Phase 7: Project Manager 📁 TODO
**Goal**: Select which projects to include in searches

**Features to Add**:
- [ ] Navigate to project manager (press `p`)
- [ ] List all available projects
- [ ] Multi-select with checkboxes (Space to toggle)
- [ ] Select all (press `a`)
- [ ] Select none (press `n`)
- [ ] Import new projects (press `i`)
- [ ] Save selection (press Enter)

**Data Source**:
- Reuse `cache.Cache` project list
- Import projects via GCP API discovery

---

### Phase 8: Navigation & Multi-View 🧭 TODO
**Goal**: Navigate between different views

**Features to Add**:
- [ ] Dashboard/home screen with quick access
- [ ] View navigation:
  - `1` or `i` - Instances (default)
  - `2` or `v` - VPN Inspector
  - `3` or `c` - Connectivity Tests
  - `4` or `p` - IP Lookup
  - `5` or `j` - Projects
- [ ] Breadcrumb trail showing current location
- [ ] Back button (Esc) to previous view
- [ ] View history (forward with `]`)

**Implementation**:
- Add view stack for navigation history
- Header showing current view path
- Global key bindings for view switching

---

### Phase 9: Advanced Features 🚀 TODO
**Goal**: Polish and additional features

**Features to Add**:
- [ ] Background auto-refresh (configurable interval)
- [ ] Real-time instance status updates
- [ ] Search across all views (press `/`)
- [ ] Copy to clipboard (selected row data)
- [ ] Export to JSON/CSV (press `e`)
- [ ] Bookmarks/favorites
- [ ] Recent connections history
- [ ] Custom filters and saved searches
- [ ] Configuration file (~/.compass-tui.yaml)
  - Last view
  - Column preferences
  - Refresh intervals
  - Theme customization

---

### Phase 10: SSH Integration 🔐 TODO
**Goal**: Seamless SSH from TUI

**Features to Add**:
- [ ] SSH to instance (press Enter on selected)
- [ ] TUI suspend during SSH session
- [ ] Resume TUI after SSH exit
- [ ] SSH options dialog before connecting
  - [ ] Port forwarding
  - [ ] SOCKS proxy
  - [ ] X11 forwarding
  - [ ] Custom SSH flags
- [ ] MIG instance selection (if MIG selected)
- [ ] IAP tunneling auto-detection
- [ ] Show connection history
- [ ] Quick reconnect to recent instance

**Implementation**:
- Use `app.Suspend(func() { ... })` to suspend TUI
- Execute SSH via `exec.Command("gcloud", ...)`
- Capture stdout/stderr to terminal
- Resume TUI on exit

---

## Keyboard Shortcuts Summary

### Global (All Views)
| Key | Action |
|-----|--------|
| `?` | Show help |
| `Ctrl+C` or `q` | Quit application |
| `Esc` | Go back / Cancel |
| `Ctrl+R` | Refresh current view |

### Instance List (Current Implementation)
| Key | Action |
|-----|--------|
| `↑`/`↓` or `j`/`k` | Navigate list |
| `Enter` | SSH to instance (TODO) |
| `/` | Filter mode (TODO) |
| `r` | Refresh (TODO) |
| `d` | Detail view (TODO) |

### Planned Global Navigation
| Key | Action |
|-----|--------|
| `1` or `i` | Instances view |
| `2` or `v` | VPN Inspector |
| `3` or `c` | Connectivity Tests |
| `4` or `l` | IP Lookup |
| `5` or `p` | Projects |
| `h` or `←` | Back in history |
| `l` or `→` | Forward in history |

### Filter Mode (Planned)
| Key | Action |
|-----|--------|
| Type | Filter text |
| `Enter` | Apply filter |
| `Esc` | Cancel filter |

### VPN Inspector (Planned)
| Key | Action |
|-----|--------|
| `Space` or `Enter` | Expand/collapse |
| `d` | Detail panel |
| `r` | Refresh |

### Connectivity Tests (Planned)
| Key | Action |
|-----|--------|
| `n` | New test |
| `Enter` | View details |
| `r` | Rerun test |
| `w` | Watch test |
| `d` or `Del` | Delete test |

---

## Integration with Existing Code

### ✅ What's Currently Integrated
- **Cache System**: `cache.Cache.GetProjects()`, `cache.Cache.GetLocationsByProject()`
- **CLI Framework**: Cobra subcommand `compass interactive`
- **GCP Client**: Created but not yet used in TUI
- **Data Model**: Uses `cache.CachedLocation` type

### 📋 What Needs Integration
- **SSH Execution**: `ssh.Client` for SSH sessions
- **VPN Data**: `gcp.Client.ListVPNOverview()`, `gcp.Client.GetVPNGateway()`
- **Connectivity Tests**: `gcp.ConnectivityClient.*` methods
- **IP Lookup**: `gcp.Client.LookupIP()`
- **Instance Details**: `gcp.Client.FindInstance()`

---

## Technical Decisions & Lessons Learned

### ❌ What Didn't Work
1. **Complex Component/PageStack Architecture**
   - **Issue**: Caused deadlock during initialization
   - **Root Cause**: Likely circular dependency or goroutine contention
   - **Files Affected**: `app.go`, `page_stack.go`, `components.go`, `instance_view.go`
   - **Decision**: Abandoned in favor of direct implementation

2. **Structured Logging in TUI**
   - **Issue**: pterm debug output corrupted terminal display
   - **Root Cause**: Logging to stderr while tview owns the terminal
   - **Solution**: Removed logger calls from TUI code

### ✅ What Works Well
1. **Direct tview.Application Usage**
   - Simple, straightforward
   - No goroutine complexity
   - Fast initialization
   - Easy to debug

2. **Cache Integration**
   - Instant data availability
   - No network calls needed for initial display
   - Works perfectly with existing cache structure

3. **Minimal Dependencies**
   - Only tview and tcell needed
   - Clean separation from rest of codebase
   - Easy to test and maintain

---

## Current File Structure

### Working Files ✅
```
internal/tui/
├── direct.go              # Main TUI implementation (94 lines)
└── styles.go              # Color schemes (partial use)

cmd/
└── interactive.go         # CLI integration (67 lines)
```

### Deprecated Files ⚠️ (Can be deleted or refactored)
```
internal/tui/
├── app.go                 # Complex app (312 lines) - CAUSES HANG
├── app_simple.go          # Intermediate (102 lines) - NOT USED
├── components.go          # Component interface (72 lines) - NOT USED
├── keys.go                # Key actions (98 lines) - NOT USED
├── page_stack.go          # Navigation stack (138 lines) - CAUSES DEADLOCK
├── instance_view.go       # Complex view (334 lines) - NOT USED
├── help_view.go           # Help screen (82 lines) - NOT USED
└── widgets/
    └── table.go           # Custom table (102 lines) - NOT USED
```

**Recommendation**: Delete unused files to avoid confusion, or refactor them to work with the direct approach.

---

## Testing the Current Implementation

### How to Use
```bash
# Build
go build -o compass

# Run interactive mode
./compass interactive

# You should see:
# - A table with cached instances
# - Arrow key navigation
# - Status bar at bottom
# - Press Ctrl+C or 'q' to quit
```

### What You Can Do Now
- ✅ View all cached instances from all projects
- ✅ Navigate with arrow keys (or j/k vim style)
- ✅ See instance name, project, zone, and status
- ✅ Quit cleanly with Ctrl+C or 'q'
- ✅ Mouse click to select rows (mouse support enabled)

### What Doesn't Work Yet
- ❌ Cannot SSH to instances (Enter does nothing)
- ❌ Cannot filter instances (/ does nothing)
- ❌ Cannot refresh data (r does nothing)
- ❌ Cannot view details (d does nothing)
- ❌ No help screen (? does nothing)
- ❌ No other views (VPN, connectivity, etc.)

---

## Next Steps (Priority Order)

### Immediate (Phase 2 - Enhanced Instance View)
1. **Add SSH capability** - Most important feature
   - Detect if Enter is pressed
   - Get selected instance from table
   - Suspend TUI with `app.Suspend()`
   - Execute gcloud SSH command
   - Resume TUI after exit

2. **Add filter mode** - Highly requested
   - Create input field (hidden by default)
   - Toggle visibility with `/` key
   - Filter table rows based on input
   - Clear filter with Esc

3. **Add color-coded status** - Visual improvement
   - Green for RUNNING
   - Red for STOPPED/TERMINATED
   - Yellow for other states
   - Requires fetching live instance data (not just cache)

4. **Add refresh capability** - Keep data fresh
   - Press `r` to refresh
   - Query GCP API for latest instance status
   - Update table with new data
   - Show loading indicator

### Short-term (Phase 3 - Help System)
5. **Implement help overlay**
   - Modal dialog showing shortcuts
   - Triggered by `?` key
   - Scrollable if needed
   - Close with Esc or ?

### Medium-term (Phases 4-7)
6. **Add VPN Inspector view**
7. **Add Connectivity Tests view**
8. **Add IP Lookup view**
9. **Add Project Manager view**

### Long-term (Phases 8-10)
10. **Multi-view navigation system**
11. **Advanced features (export, bookmarks, etc.)**
12. **Configuration persistence**

---

## Design Principles (Based on k9s)

### UI/UX Patterns
- **Keyboard-first**: Everything accessible via keyboard
- **Vim-inspired**: j/k navigation, Esc to cancel, / for search
- **Color-coded status**: Green=good, Red=bad, Yellow=warning
- **Minimal chrome**: Focus on content, not decoration
- **Contextual help**: Always accessible with ?
- **Status hints**: Bottom bar shows available actions

### Color Scheme (k9s-inspired)
```go
// From styles.go
Background:     Black
Foreground:     White
Border:         DarkCyan
StatusOK:       Green
StatusError:    Red
StatusWarning:  Yellow
StatusInfo:     DodgerBlue
TableHeader:    DarkCyan background, Black text
TableSelected:  DarkCyan background, White text
Title:          Aqua text
Breadcrumb:     Gray text
```

### Keyboard Philosophy
- **Single-key actions**: Common actions use single keys (r, d, v, etc.)
- **Ctrl+Key for global**: Ctrl+C quit, Ctrl+R refresh
- **Shift+Key for sorting**: Shift+N sort by name, Shift+A sort by age
- **/ for filter/search**: Universal pattern
- **? for help**: Universal pattern
- **Esc for back/cancel**: Universal pattern

---

## Performance Considerations

### Current Implementation
- **Instant startup**: No network calls, uses cache only
- **Fast rendering**: tview is highly optimized
- **Low memory**: Only loads cached data (~KB for typical cache)
- **No goroutines**: Simplified direct approach avoids concurrency issues

### Future Optimizations Needed
- **Background refresh**: Poll GCP APIs without blocking UI
- **Virtualized scrolling**: For large instance lists (1000+)
- **Debounced filtering**: Don't filter on every keystroke
- **Lazy loading**: Load details only when requested
- **Connection pooling**: Reuse GCP client connections

---

## Dependencies

### Added to go.mod
```go
require (
    github.com/rivo/tview v0.42.0
    github.com/gdamore/tcell/v2 v2.9.0
)
```

### Transitive Dependencies (auto-added)
```go
github.com/gdamore/encoding v1.0.1
github.com/lucasb-eyer/go-colorful v1.2.0
github.com/mattn/go-runewidth v0.0.19  // Already present
github.com/rivo/uniseg v0.4.7
```

---

## Known Issues & Workarounds

### Issue 1: Complex App Architecture Deadlock ❌
**Symptom**: Program hangs at "Creating TUI app..." and cannot be stopped with Ctrl+C

**Root Cause**: The PageStack/Component architecture with goroutines and channels causes a deadlock during initialization. Likely related to:
- `pageStack.Push()` calling `component.Start()` which launches goroutines
- `component.Start()` calling `app.QueueUpdateDraw()` before app.Run() is called
- Circular dependencies in the initialization order

**Workaround**: Use `RunDirect()` instead of the complex App structure

**Future Fix Options**:
1. Refactor to remove goroutines from component initialization
2. Use channels instead of direct method calls
3. Lazy-initialize components only after app.Run() is called
4. Simplify the component lifecycle (remove Start/Stop)

### Issue 2: Logging Corruption ❌
**Symptom**: Terminal display is corrupted with debug log messages

**Root Cause**: pterm writes to stderr which interferes with tcell's terminal control

**Workaround**: Remove all logger calls from TUI code

**Future Fix**: Redirect logs to a file when in TUI mode, or use a separate log view

### Issue 3: No Live Instance Status ⚠️
**Symptom**: All instances show "CACHED" status regardless of actual state

**Root Cause**: TUI only reads from cache, doesn't query GCP API

**Workaround**: None yet - this is expected behavior

**Future Fix**: Add background refresh that queries GCP API for latest status

---

## Success Criteria

### MVP (Current) ✅
- [x] TUI launches successfully
- [x] Displays cached instances
- [x] Keyboard navigation works
- [x] Can quit with Ctrl+C
- [x] Clean display without log corruption

### Version 1.0 (Target)
- [ ] SSH to instances from TUI
- [ ] Filter instances by name/project/zone
- [ ] Refresh instance status from GCP
- [ ] Help screen with keyboard shortcuts
- [ ] Color-coded instance status
- [ ] Stable, no crashes or hangs

### Version 2.0 (Future)
- [ ] VPN Inspector view
- [ ] Connectivity Tests view
- [ ] IP Lookup view
- [ ] Project Manager view
- [ ] Multi-view navigation
- [ ] Background auto-refresh

---

## Timeline Estimate

Based on simplified architecture:

- **Phase 2** (Enhanced Instance View): 2-3 days
  - SSH: 4-6 hours
  - Filter: 3-4 hours
  - Color status: 2-3 hours
  - Refresh: 2-3 hours
  - Testing: 4-6 hours

- **Phase 3** (Help System): 1 day
- **Phase 4** (VPN Inspector): 2-3 days
- **Phase 5** (Connectivity Tests): 2-3 days
- **Phase 6** (IP Lookup): 1-2 days
- **Phase 7** (Project Manager): 1-2 days
- **Phase 8** (Navigation): 2-3 days
- **Phase 9** (Advanced Features): 3-5 days
- **Phase 10** (SSH Integration): Already included in Phase 2

**Total Estimate**: 14-23 days (3-5 weeks) for full implementation

**MVP to v1.0**: 2-3 days (just Phase 2 + 3)

---

## Documentation

### User Documentation Needed
- [ ] README section for TUI mode
- [ ] Keyboard shortcut reference
- [ ] Screenshots/GIFs of TUI in action
- [ ] Comparison: CLI vs TUI use cases

### Developer Documentation Needed
- [ ] Architecture overview (current simplified approach)
- [ ] How to add new views
- [ ] How to add keyboard shortcuts
- [ ] Testing guide

---

## Conclusion

The TUI implementation is **working at MVP level** using a simplified direct approach. The complex component-based architecture was causing deadlocks and has been abandoned in favor of a straightforward tview implementation.

**Current State**: Basic instance list view is functional and stable.

**Next Priority**: Add SSH capability (Phase 2) to make the TUI actually useful for daily work.

**Long-term Vision**: Full-featured TUI with multiple views (VPN, connectivity tests, IP lookup) providing a comprehensive GCP management interface, similar to k9s for Kubernetes.

---

*Last Updated: 2025-11-04*
*Status: MVP Working, Phase 2 Ready to Start*
