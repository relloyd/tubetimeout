# Use the official Ubuntu base image
FROM ubuntu:latest

# Set the non-interactive frontend to avoid prompts during package installation
ENV DEBIAN_FRONTEND=noninteractive

# Update package lists and install necessary packages
RUN apt-get update && \
    apt-get install -y \
    nftables \
    man-db \
    manpages \
    manpages-dev \
    build-essential \
    libnetfilter-queue-dev \
    libnfnetlink-dev \
    python3-pip \
    golang \
    curl \
    && apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Set Go environment variables
ENV GOPATH=/root/go
ENV PATH=$GOPATH/bin:/usr/local/go/bin:$PATH

# Set the default command to launch a shell
CMD ["/bin/bash"]
