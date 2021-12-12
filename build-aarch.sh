#!/usr/bin/bash
sudo apt update && apt upgrade -y
sudo reboot now

# install go
wget https://golang.org/dl/go1.17.2.linux-arm64.tar.gz
sudo tar -C /usr/local -xzf go1.17.2.linux-arm64.tar.gz
rm go1.17.2.linux-arm64.tar.gz

# setup path for go
echo "PATH=$PATH:/usr/local/go/bin
GOPATH=$HOME/go" >> ~/.profile
source ~/.profile

# install go c compiler
sudo apt install gcc -y

# check if go and c installed ok
go version
gcc -v

env GOOS=linux GOARCH=arm64 go build -o idena-go_arm64
