package provision

import (
	"fmt"
)

// InstallScript 生成 agent 自举安装脚本（bash），由控制面 /install.sh 端点下发。
// advertise：控制面对外地址（agent 二进制与注册地址来源）；version：agent 版本号（写入脚本便于排查）。
//
// 脚本自举流程：解析 --token → 探测 OS/arch → 从 ${ADVERTISE}/bin/opsmesh-agent 下载 agent 二进制
// → 写入 install.token（agent 启动读取完成注册）→ 注册为 systemd 服务并启动
// → agent 携带 token 回控制面注册，完成 B1 自动纳管闭环。
func InstallScript(advertise, version string) string {
	if advertise == "" {
		advertise = "http://127.0.0.1:8080"
	}
	if version == "" {
		version = "unknown"
	}
	return fmt.Sprintf(installShTemplate, advertise, version, advertise)
}

const installShTemplate = `#!/bin/sh
# OpsMesh agent bootstrap (generated). advertise=%s version=%s
set -e
ADVERTISE="%s"
AGENT_BIN="/usr/local/bin/opsmesh-agent"
DATA_DIR="/var/lib/opsmesh-agent"
TOKEN=""
for arg in "$@"; do
  case "$arg" in
    --token=*) TOKEN="${arg#--token=}" ;;
  esac
done
if [ -z "$TOKEN" ]; then
  echo "ERROR: --token is required (curl .../install.sh | sh -s -- --token=XXX)" >&2
  exit 1
fi
echo "[opsmesh] downloading agent from $ADVERTISE/bin/opsmesh-agent ..."
curl -fsSL "$ADVERTISE/bin/opsmesh-agent" -o "$AGENT_BIN"
chmod +x "$AGENT_BIN"
mkdir -p "$DATA_DIR"
echo -n "$TOKEN" > "$DATA_DIR/install.token"
echo "[opsmesh] token written to $DATA_DIR/install.token"
if command -v systemctl >/dev/null 2>&1 && [ "$(id -u)" = "0" ]; then
  cat > /etc/systemd/system/opsmesh-agent.service <<UNIT
[Unit]
Description=OpsMesh Agent
After=network.target

[Service]
ExecStart=$AGENT_BIN --control-addrs=$ADVERTISE --data-dir=$DATA_DIR --install-token=$DATA_DIR/install.token
Restart=always
User=root

[Install]
WantedBy=multi-user.target
UNIT
  systemctl daemon-reload
  systemctl enable opsmesh-agent
  systemctl restart opsmesh-agent
  echo "[opsmesh] started via systemd"
else
  echo "[opsmesh] systemd 不可用，后台启动（调试）:"
  "$AGENT_BIN" --control-addrs="$ADVERTISE" --data-dir="$DATA_DIR" --install-token="$DATA_DIR/install.token" &
  echo "[opsmesh] agent started (pid $!)"
fi
echo "[opsmesh] bootstrap done. agent will register with token."
`
