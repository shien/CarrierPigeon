#!/bin/sh
# Install Docker Engine inside WSL2 (Ubuntu) and add the current user to the docker group.
# After running, restart WSL (`wsl --shutdown` on Windows, then reopen) so the group
# membership takes effect, then start the daemon with: sudo service docker start

set -e

curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker "$USER"

echo "Docker installed. Restart WSL (wsl --shutdown) or log out/in, then run:"
echo "  sudo service docker start"
echo "  docker run hello-world"
