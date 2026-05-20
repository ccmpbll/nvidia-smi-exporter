#!/bin/bash
# Builds the nvidia-smi-exporter binary and packages it as a Slackware .tgz
# suitable for Unraid's installpkg. Outputs to packages/<version>/.
#
# Usage: ./unraid/build.sh <version>
#   e.g.: ./unraid/build.sh 2026.05.12

set -e

VERSION="${1:-$(date +%Y.%m.%d)}"
PKGNAME="nvidia-smi-exporter"
PKGARCH="x86_64"
PKGBUILD="1"
FULLNAME="${PKGNAME}-${VERSION}-${PKGARCH}-${PKGBUILD}"
PKGDIR="packages/${VERSION}/pkg"
OUTDIR="packages/${VERSION}"

echo "Building ${FULLNAME}..."

# Ensure go modules are tidy before building
( cd src && go mod tidy )

# Compile static binary for Unraid (x86_64 Linux)
( cd src && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 \
  go build -ldflags="-s -w" -o "../${PKGNAME}" . )

mkdir -p \
  "${PKGDIR}/usr/bin" \
  "${PKGDIR}/etc/rc.d" \
  "${PKGDIR}/usr/local/emhttp/plugins/${PKGNAME}/images" \
  "${PKGDIR}/install"

# Icon
install -m 644 "unraid/icon.png" "${PKGDIR}/usr/local/emhttp/plugins/${PKGNAME}/images/${PKGNAME}.png"

# README for Plugins tab description
install -m 644 "unraid/README.md" "${PKGDIR}/usr/local/emhttp/plugins/${PKGNAME}/README.md"

# Binary
install -m 755 "${PKGNAME}" "${PKGDIR}/usr/bin/${PKGNAME}"
rm "${PKGNAME}"

# rc script
cat > "${PKGDIR}/etc/rc.d/rc.${PKGNAME}" << 'RC_EOF'
#!/bin/bash
# /etc/rc.d/rc.nvidia-smi-exporter — start/stop/restart/status

DAEMON=/usr/bin/nvidia-smi-exporter
PIDFILE=/var/run/nvidia-smi-exporter.pid
LOGFILE=/var/log/nvidia-smi-exporter.log
CFG=/boot/config/plugins/nvidia-smi-exporter/nvidia-smi-exporter.cfg
LOGNAME=nvidia-smi-exporter

[ -f "$CFG" ] && source "$CFG"
EXPORTER_PORT=":${PORT:-9202}"

nvidia_smi_check() {
  if [ ! -x /usr/bin/nvidia-smi ]; then
    logger -t "$LOGNAME" "ERROR: /usr/bin/nvidia-smi not found. Install the 'Nvidia-Driver' plugin from Community Applications first."
    return 1
  fi
}

nvidia_start() {
  nvidia_smi_check || return 1
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    logger -t "$LOGNAME" "Already running (pid $(cat "$PIDFILE"))"
    return 0
  fi
  logger -t "$LOGNAME" "Starting on ${EXPORTER_PORT}"
  EXPORTER_PORT="$EXPORTER_PORT" NVIDIA_SMI_PATH=/usr/bin/nvidia-smi \
    nohup "$DAEMON" >> "$LOGFILE" 2>&1 &
  echo $! > "$PIDFILE"
}

nvidia_stop() {
  if [ -f "$PIDFILE" ]; then
    local pid
    pid=$(cat "$PIDFILE")
    if kill "$pid" 2>/dev/null; then
      logger -t "$LOGNAME" "Stopping (pid $pid)..."
      local i=0
      while kill -0 "$pid" 2>/dev/null && [ "$i" -lt 10 ]; do
        sleep 0.5
        i=$((i + 1))
      done
      logger -t "$LOGNAME" "Stopped"
    fi
    rm -f "$PIDFILE"
  fi
}

nvidia_status() {
  if [ -f "$PIDFILE" ] && kill -0 "$(cat "$PIDFILE")" 2>/dev/null; then
    echo "nvidia-smi-exporter is running (pid $(cat "$PIDFILE"))"
  else
    echo "nvidia-smi-exporter is stopped"
  fi
}

case "$1" in
  start)   nvidia_start ;;
  stop)    nvidia_stop ;;
  restart) nvidia_stop; nvidia_start ;;
  status)  nvidia_status ;;
  *)       echo "Usage: $0 {start|stop|restart|status}" ;;
esac
RC_EOF
chmod 755 "${PKGDIR}/etc/rc.d/rc.${PKGNAME}"

# Unraid settings page
cat > "${PKGDIR}/usr/local/emhttp/plugins/${PKGNAME}/${PKGNAME}.page" << 'PAGE_EOF'
Menu="Utilities"
Type="xmenu"
Title="Nvidia-SMI Exporter"
Icon="nvidia-smi-exporter.png"
---
<?php
$cfg_file = '/boot/config/plugins/nvidia-smi-exporter/nvidia-smi-exporter.cfg';
$cfg = ['PORT' => '9202'];
if (file_exists($cfg_file)) {
  foreach (file($cfg_file) as $line) {
    if (preg_match('/^(\w+)=(.*)$/', trim($line), $m)) {
      $cfg[$m[1]] = $m[2];
    }
  }
}

$port    = htmlspecialchars($cfg['PORT']);
$pidfile = '/var/run/nvidia-smi-exporter.pid';
$running = file_exists($pidfile) && posix_getpgid((int)file_get_contents($pidfile)) !== false;
$status  = $running ? '<span style="color:green">&#9679; Running</span>'
                    : '<span style="color:red">&#9679; Stopped</span>';

$gpus = '';
if ($running && is_executable('/usr/bin/nvidia-smi')) {
  $gpus = htmlspecialchars(shell_exec('/usr/bin/nvidia-smi --query-gpu=index,name,driver_version --format=csv,noheader 2>/dev/null') ?? '');
}

if ($_SERVER['REQUEST_METHOD'] === 'POST') {
  $action = $_POST['action'] ?? '';
  if (in_array($action, ['start','stop','restart'], true)) {
    shell_exec("/etc/rc.d/rc.nvidia-smi-exporter $action");
  }
  if ($action === 'save') {
    $new_port = (int)($_POST['port'] ?? 9202);
    @mkdir(dirname($cfg_file), 0755, true);
    file_put_contents($cfg_file, "PORT=$new_port\n");
  }
  header('Location: '.$_SERVER['REQUEST_URI']);
  exit;
}
?>
<div style="width:49%;float:left;padding-right:1%">
  <p><strong>Status:</strong> <?=$status?></p>
  <?php if ($gpus): ?>
  <pre style="background:#1e1e1e;color:#ccc;padding:8px;border-radius:4px"><?=$gpus?></pre>
  <?php endif; ?>
  <form method="post">
    <button name="action" value="start">Start</button>
    <button name="action" value="stop">Stop</button>
    <button name="action" value="restart">Restart</button>
  </form>
</div>
<div style="width:49%;float:right">
  <form method="post">
    <table>
      <tr>
        <td><label for="port">Listen Port:</label></td>
        <td><input type="number" id="port" name="port" value="<?=$port?>" min="1" max="65535" style="width:80px"></td>
      </tr>
      <tr><td colspan="2"><button name="action" value="save">Save</button></td></tr>
    </table>
  </form>
  <p style="font-size:0.85em;color:#888">
    Metrics endpoint: <a href="http://<?=$_SERVER['SERVER_ADDR']?>:<?=$port?>/metrics" target="_blank">
    http://<?=$_SERVER['SERVER_ADDR']?>:<?=$port?>/metrics</a>
  </p>
</div>
PAGE_EOF

# slack-desc
cat > "${PKGDIR}/install/slack-desc" << DESC_EOF
nvidia-smi-exporter: nvidia-smi-exporter (Prometheus exporter for NVIDIA GPU metrics)
nvidia-smi-exporter:
nvidia-smi-exporter: Exports NVIDIA GPU metrics via nvidia-smi in Prometheus text format.
nvidia-smi-exporter: Supports all GPU architectures including Blackwell (RTX 5000-series,
nvidia-smi-exporter: driver 595+). Listens on port 9202 by default.
nvidia-smi-exporter:
nvidia-smi-exporter: Requires: Nvidia-Driver plugin (Community Applications)
nvidia-smi-exporter:
nvidia-smi-exporter:
nvidia-smi-exporter:
nvidia-smi-exporter:
DESC_EOF

# post-install script
cat > "${PKGDIR}/install/doinst.sh" << 'DOINST_EOF'
#!/bin/bash
chmod 755 /etc/rc.d/rc.nvidia-smi-exporter
DOINST_EOF

mkdir -p "${OUTDIR}"

# Build the Slackware-compatible package using portable tar (makepkg is Slackware-only)
OUTPATH="$(pwd)/${OUTDIR}/${FULLNAME}.tgz"
( cd "${PKGDIR}" && tar -czf "$OUTPATH" . )

md5sum "${OUTPATH}" | awk '{print $1}' > "${OUTDIR}/${FULLNAME}.md5"

echo "Done: ${OUTDIR}/${FULLNAME}.tgz"
echo "MD5:  $(cat "${OUTDIR}/${FULLNAME}.md5")"
