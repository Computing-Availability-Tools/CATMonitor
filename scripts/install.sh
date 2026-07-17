#!/bin/bash
set -e

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/catmonitor"
DATA_DIR="/var/lib/catmonitor/data"
SERVICE_DIR="/etc/systemd/system"

echo "Installing CATMonitor..."

# Copy binary
cp bin/catmonitor ${INSTALL_DIR}/catmonitor
echo "Binary installed to ${INSTALL_DIR}/catmonitor"

# Create config directory
mkdir -p ${CONFIG_DIR}
if [ ! -f ${CONFIG_DIR}/catmonitor.yaml ]; then
    cp configs/catmonitor.yaml ${CONFIG_DIR}/catmonitor.yaml
    echo "Config installed to ${CONFIG_DIR}/catmonitor.yaml"
fi
# Metrics catalog (default selection: High/Medium + static identity).
if [ ! -f ${CONFIG_DIR}/metrics.yaml ]; then
    cp configs/metrics.yaml ${CONFIG_DIR}/metrics.yaml
    echo "Metrics catalog installed to ${CONFIG_DIR}/metrics.yaml"
fi

# Create data directory
mkdir -p ${DATA_DIR}
echo "Data directory created at ${DATA_DIR}"

# Create systemd service file
cat > ${SERVICE_DIR}/catmonitor.service << EOF
[Unit]
Description=CATMonitor - Server Metrics Collector
After=network.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/catmonitor daemon -c ${CONFIG_DIR}/catmonitor.yaml
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
echo "Systemd service installed to ${SERVICE_DIR}/catmonitor.service"

# Reload systemd
systemctl daemon-reload
echo "Systemd daemon reloaded"

echo ""
echo "Installation complete. Use the following commands:"
echo "  sudo systemctl start catmonitor    # Start service"
echo "  sudo systemctl stop catmonitor     # Stop service"
echo "  sudo systemctl status catmonitor   # Check status"
echo "  sudo journalctl -u catmonitor -f   # View logs"
