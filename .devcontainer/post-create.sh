#!/bin/bash

# This script runs after the devcontainer is created
# It sets up the development environment

set -e

echo "🚀 Running post-create setup..."

# Colors for output
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Download Go dependencies
echo -e "${BLUE}📦 Downloading Go dependencies...${NC}"
go mod download

# Verify Go installation
echo -e "${BLUE}🔍 Verifying Go installation...${NC}"
go version

# Install/update Go tools
echo -e "${BLUE}🔧 Installing/updating Go development tools...${NC}"
go install golang.org/x/tools/cmd/goimports@latest
go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest
go install github.com/go-delve/delve/cmd/dlv@latest
go install gotest.tools/gotestsum@latest

# Verify tools are installed
echo -e "${BLUE}✅ Verifying tools installation...${NC}"
which goimports golangci-lint dlv gotestsum

# Build the project to verify everything works
echo -e "${BLUE}🔨 Building the project...${NC}"
if go build -v ./...; then
    echo -e "${GREEN}✅ Build successful!${NC}"
else
    echo -e "${YELLOW}⚠️  Build failed. You may need to fix dependencies.${NC}"
fi

# Run tests to ensure everything is working
echo -e "${BLUE}🧪 Running tests...${NC}"
if go test -v ./... 2>&1 | head -20; then
    echo -e "${GREEN}✅ Tests are running!${NC}"
else
    echo -e "${YELLOW}⚠️  Some tests may need attention.${NC}"
fi

# Set up git hooks if .git directory exists
if [ -d ".git" ]; then
    echo -e "${BLUE}🎣 Setting up git hooks...${NC}"
    # Pre-commit hook to run tests
    cat > .git/hooks/pre-commit << 'EOF'
#!/bin/bash
echo "Running tests before commit..."
go test ./...
if [ $? -ne 0 ]; then
    echo "Tests failed. Commit aborted."
    exit 1
fi
EOF
    chmod +x .git/hooks/pre-commit
    echo -e "${GREEN}✅ Git hooks configured${NC}"
fi

# Create helpful aliases
echo -e "${BLUE}⚙️  Setting up shell aliases...${NC}"
cat >> ~/.zshrc << 'EOF'

# Project-specific aliases
alias gt='go test ./...'
alias gtv='go test -v ./...'
alias gtc='go test -cover ./...'
alias gb='go build ./...'
alias gf='go fmt ./...'
alias gl='golangci-lint run ./...'
alias goclean='go clean -cache -testcache -modcache'

# Task aliases (if using Taskfile)
alias t='task'
alias tl='task --list'

# Coverage helpers
alias cover='go test -coverprofile=coverage.out ./... && go tool cover -html=coverage.out'
alias coverfunc='go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out'

EOF

echo -e "${GREEN}✅ Shell aliases added to ~/.zshrc${NC}"

# Display summary
echo ""
echo -e "${GREEN}╔════════════════════════════════════════════╗${NC}"
echo -e "${GREEN}║   Development Container Setup Complete!   ║${NC}"
echo -e "${GREEN}╔════════════════════════════════════════════╗${NC}"
echo ""
echo -e "${BLUE}Available commands:${NC}"
echo -e "  ${YELLOW}gt${NC}      - Run tests"
echo -e "  ${YELLOW}gtv${NC}     - Run tests (verbose)"
echo -e "  ${YELLOW}gtc${NC}     - Run tests with coverage"
echo -e "  ${YELLOW}gb${NC}      - Build project"
echo -e "  ${YELLOW}gl${NC}      - Run linter"
echo -e "  ${YELLOW}cover${NC}   - Generate HTML coverage report"
echo ""
echo -e "${BLUE}Tools installed:${NC}"
echo -e "  • Go $(go version | cut -d' ' -f3)"
echo -e "  • golangci-lint"
echo -e "  • delve (dlv)"
echo -e "  • goimports"
echo -e "  • gotestsum"
echo -e "  • gcloud SDK"
echo ""
echo -e "${GREEN}Happy coding! 🎉${NC}"
