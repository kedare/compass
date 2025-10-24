# Use the official Go image as base
FROM golang:1.23-bookworm

# Avoid warnings by switching to noninteractive
ENV DEBIAN_FRONTEND=noninteractive

# Configure apt and install packages
RUN apt-get update \
    && apt-get -y install --no-install-recommends \
    # Common utilities
    apt-utils \
    dialog \
    openssh-client \
    less \
    curl \
    wget \
    unzip \
    nano \
    vim \
    # Build tools
    build-essential \
    cmake \
    # Version control
    git \
    git-lfs \
    # Shell
    zsh \
    # Network tools for GCP development
    iputils-ping \
    dnsutils \
    netcat-traditional \
    traceroute \
    # SSL/TLS tools
    ca-certificates \
    # Python for scripts
    python3 \
    python3-pip \
    # Clean up
    && apt-get autoremove -y \
    && apt-get clean -y \
    && rm -rf /var/lib/apt/lists/*

# Install GCP SDK
RUN echo "deb [signed-by=/usr/share/keyrings/cloud.google.gpg] https://packages.cloud.google.com/apt cloud-sdk main" | \
    tee -a /etc/apt/sources.list.d/google-cloud-sdk.list \
    && curl https://packages.cloud.google.com/apt/doc/apt-key.gpg | \
    apt-key --keyring /usr/share/keyrings/cloud.google.gpg add - \
    && apt-get update \
    && apt-get install -y google-cloud-sdk google-cloud-sdk-gke-gcloud-auth-plugin \
    && apt-get clean -y \
    && rm -rf /var/lib/apt/lists/*

# Install Go tools
RUN go install golang.org/x/tools/cmd/goimports@latest \
    && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest \
    && go install github.com/go-delve/delve/cmd/dlv@latest \
    && go install github.com/cosmtrek/air@latest \
    && go install github.com/go-task/task/v3/cmd/task@latest \
    && go install gotest.tools/gotestsum@latest \
    && go install github.com/kisielk/errcheck@latest \
    && go install honnef.co/go/tools/cmd/staticcheck@latest \
    && go install github.com/segmentio/golines@latest

# Install additional useful tools
RUN go install github.com/vektra/mockery/v2@latest \
    && go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Create a non-root user
ARG USERNAME=vscode
ARG USER_UID=1000
ARG USER_GID=$USER_UID

RUN groupadd --gid $USER_GID $USERNAME \
    && useradd --uid $USER_UID --gid $USER_GID -m $USERNAME \
    && apt-get update \
    && apt-get install -y sudo \
    && echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME

# Set up Go workspace for the user
RUN mkdir -p /go/pkg /go/bin /go/src \
    && chown -R $USERNAME:$USERNAME /go

# Switch back to dialog for any ad-hoc use of apt-get
ENV DEBIAN_FRONTEND=dialog

# Configure git to trust the workspace directory
RUN git config --system --add safe.directory '*'

# Set the default user
USER $USERNAME

# Set up shell
RUN sh -c "$(curl -fsSL https://raw.githubusercontent.com/ohmyzsh/ohmyzsh/master/tools/install.sh)" "" --unattended

# Configure zsh
RUN echo 'export GOPATH=/go' >> ~/.zshrc \
    && echo 'export PATH=$PATH:/go/bin' >> ~/.zshrc \
    && echo 'export ZSH_THEME="robbyrussell"' >> ~/.zshrc \
    && echo 'plugins=(git golang docker kubectl gcloud)' >> ~/.zshrc

# Set working directory
WORKDIR /workspace

# Default command
CMD ["/bin/zsh"]
