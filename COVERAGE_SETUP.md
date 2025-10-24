# Coverage Setup - Complete Guide

This project has **visual test coverage** directly in VSCode! Here's everything you need to know.

## ✅ What's Already Configured

Your VSCode is pre-configured to show coverage with:
- **🟢 Green gutter blocks** = Covered code
- **🔴 Red gutter blocks** = Uncovered code
- **Automatic coverage display** after running tests
- **Coverage on save** enabled

## 🚀 Quick Start - 3 Ways to See Coverage

### Method 1: Single Package with Visual Coverage (Best!)

**Perfect for: Working on a specific package**

1. Open any Go file (e.g., `internal/output/spinner.go`)
2. Press `F5`
3. Select **"Test Current Package with Coverage"**
4. ✅ See green/red gutters appear in the editor!

**What happens:**
- Tests run for the current package
- Coverage is calculated
- Green/red gutters appear showing what's covered
- `coverage.out` file is created

### Method 2: All Packages with Coverage Summary

**Perfect for: Checking overall project coverage**

1. Press `Ctrl+Shift+P` (Cmd+Shift+P on Mac)
2. Type "Run Task"
3. Select **"go: test all packages with coverage and show"**
4. ✅ See all test results + coverage summary!

**Output:**
```
✅ All tests pass
📊 Coverage Summary:
internal/logger/logger.go:32:    SetLevel         100.0%
internal/output/spinner.go:57:   Start            90.0%
internal/output/vpn.go:194:      displayVPNText   85.0%
...
total:                           (statements)     65.5%
```

### Method 3: Command Line

**Perfect for: Quick checks or CI/CD**

```bash
# Run all tests with coverage
go test -coverprofile=coverage.out ./...

# See summary
go tool cover -func=coverage.out | tail -1

# Open HTML report (manual)
go tool cover -html=coverage.out
```

## 🎨 Understanding Visual Coverage

### What You'll See in the Editor

```
Line | Gutter | Code
-----|--------|------------------------------------------------
  32 | 🟢     | func NewSpinner(message string) *Spinner {
  33 | 🟢     |     writer := os.Stderr
  34 | 🟢     |     enabled := term.IsTerminal(int(writer.Fd()))
  35 | 🔴     |     if someRareCondition {  // NOT TESTED!
  36 | 🔴     |         // This never runs
  37 | 🔴     |     }
  38 | 🟢     |     return &Spinner{...}
```

- **🟢 Green** = This line is executed by tests ✅
- **🔴 Red** = This line is NOT executed by tests ❌
- **No mark** = Not executable (comments, blank lines, declarations)

### Coverage Appears Automatically!

With `go.coverOnSave: true` enabled, coverage appears when:
- ✅ You run "Test Current Package with Coverage" (F5)
- ✅ You use the Test Explorer and click "Run Test"
- ✅ You run tests from the command line and `coverage.out` exists

## 📊 Current Project Coverage

Run this to see current coverage:
```bash
go test -cover ./...
```

**Last known coverage:**
| Package | Coverage |
|---------|----------|
| internal/logger | 90.2% |
| internal/ssh | 74.4% |
| internal/output | 72.0% |
| internal/cache | 54.4% |
| internal/update | 52.9% |
| cmd | 21.4% |
| internal/gcp | 20.9% |
| internal/version | 100.0% |

## 🎯 Workflow: Test-Driven Development with Coverage

### Workflow 1: Writing New Code

```
1. Write new function in spinner.go
   🔴 All lines are RED (no tests)

2. Write test in spinner_test.go
   func TestNewFunction(t *testing.T) { ... }

3. Press F5 → "Test Current Package with Coverage"
   🟢 Some lines turn GREEN!

4. Add more test cases for edge cases
5. Press F5 again
   🟢 More lines turn GREEN!

6. Repeat until all lines are GREEN
   ✅ Function fully tested!
```

### Workflow 2: Fixing Bugs

```
1. Open file with bug (e.g., vpn.go)
2. Press F5 → "Test Current Package with Coverage"
3. Look for 🔴 RED lines near the bug
4. Those lines might not have tests!
5. Add test that reproduces the bug
6. Fix the bug
7. Press F5 → Verify fix and coverage
```

### Workflow 3: Improving Coverage

```
1. Run: Ctrl+Shift+P → "test all packages with coverage and show"
2. Find package with low coverage (e.g., 21.4%)
3. Open source file in that package
4. Press F5 → "Test Current Package with Coverage"
5. Look for 🔴 RED functions/lines
6. Add tests for those functions
7. Repeat until coverage is acceptable (>70%)
```

## ⚙️ Configuration Details

### VSCode Settings (`.vscode/settings.json`)

```json
{
  "go.coverOnSave": true,              // Auto-show coverage after tests
  "go.coverOnSingleTest": true,        // Show coverage after single test
  "go.coverOnTestPackage": true,       // Show coverage after package tests
  "go.coverageDecorator": {
    "type": "gutter",                  // Show in gutter (left side)
    "coveredGutterStyle": "blockgreen", // Green blocks
    "uncoveredGutterStyle": "blockred" // Red blocks
  },
  "go.coverageOptions": "showBothCoveredAndUncoveredCode"
}
```

### Debug Configuration (`.vscode/launch.json`)

```json
{
  "name": "Test Current Package with Coverage",
  "buildFlags": "-cover",              // Build with coverage support
  "args": [
    "-test.coverprofile=coverage.out"  // Write coverage file
  ]
}
```

## 🔧 Available Commands

### VSCode Commands (Ctrl+Shift+P)

| Command | What It Does |
|---------|-------------|
| **Go: Toggle Test Coverage in Current Package** | Show/hide coverage |
| **Go: Apply Coverage Profile** | Load coverage from file |
| **Go: Remove Coverage Profile** | Clear coverage display |

### Tasks (Ctrl+Shift+P → Run Task)

| Task | What It Does |
|------|-------------|
| **go: test all packages with coverage and show** | Run all tests, show summary |
| **go: test with coverage** | Run all tests, create coverage.out |
| **go: test current package with coverage (no debug)** | Test + HTML report |
| **Show Coverage** | Open HTML report in browser |

### Terminal Commands

```bash
# Run tests with coverage
go test -coverprofile=coverage.out ./...

# View in terminal (summary)
go tool cover -func=coverage.out

# View in terminal (last line = total)
go tool cover -func=coverage.out | tail -1

# Open HTML report in browser
go tool cover -html=coverage.out

# Per-package coverage
go test -cover ./...

# Verbose with coverage
go test -v -cover ./...
```

## 🎓 Tips & Tricks

### Tip 1: Focus on One Package

```
1. Open a file in the package (e.g., spinner.go)
2. F5 → "Test Current Package with Coverage"
3. Focus on that package only
✅ Faster feedback loop!
```

### Tip 2: Coverage + Breakpoints

```
1. Set breakpoint in source code
2. F5 → "Test Current Package with Coverage"
3. Debugger stops at breakpoint
4. Step through code
5. After debugging, see coverage!
✅ Understand code flow + coverage!
```

### Tip 3: Find Untested Code Fast

```
1. F5 → "Test Current Package with Coverage"
2. Scroll through file
3. Look for 🔴 RED sections
4. Those are your testing targets!
✅ Quick visual scan!
```

### Tip 4: Before Committing

```bash
# Check coverage of what you changed
go test -cover ./internal/output

# Must be >70% for new code
```

### Tip 5: Hide Coverage Temporarily

```
Ctrl+Shift+P → "Go: Remove Coverage Profile"
```

Coverage gutters disappear. Run tests again to bring them back.

## 🐛 Troubleshooting

### Coverage Not Showing?

**Solution 1:** Reload window
```
Ctrl+Shift+P → "Developer: Reload Window"
```

**Solution 2:** Check coverage file exists
```bash
ls -la coverage.out
```

**Solution 3:** Manually apply coverage
```
Ctrl+Shift+P → "Go: Apply Coverage Profile"
Select: coverage.out
```

### Coverage Showing Old Data?

**Solution:** Remove and regenerate
```
Ctrl+Shift+P → "Go: Remove Coverage Profile"
F5 → "Test Current Package with Coverage"
```

### Can't See Gutter Decorations?

**Check settings:**
```json
{
  "go.coverageDecorator": {
    "type": "gutter"  // Must be "gutter", not "highlight"
  }
}
```

## 📚 Additional Resources

- **Coverage Guide**: `.vscode/COVERAGE_GUIDE.md` - Detailed usage
- **Visual Examples**: `.vscode/COVERAGE_VISUAL_EXAMPLE.md` - See examples
- **VSCode README**: `.vscode/README.md` - All VSCode features
- **Quick Reference**: `.vscode/QUICK_REFERENCE.md` - Cheat sheet

## ✨ Summary

**You now have:**
- ✅ Visual coverage with green/red gutters
- ✅ Automatic coverage display after tests
- ✅ Multiple ways to run tests with coverage
- ✅ Complete documentation

**To start:**
1. Open `internal/output/spinner.go`
2. Press `F5`
3. Select "Test Current Package with Coverage"
4. See the green and red gutters! 🎨

**That's it!** Happy testing! 🧪✅
