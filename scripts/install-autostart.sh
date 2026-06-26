#!/usr/bin/env sh
set -eu

APP_NAME="gpt-model-proxy"
SYSTEMD_UNIT="gpt-model-proxy.service"
LAUNCHD_LABEL="com.backfire.gpt-model-proxy"
ROOT_DIR=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

require_binary() {
  if [ ! -x "$HOME/.local/bin/$APP_NAME" ]; then
    printf '%s\n' "missing executable: $HOME/.local/bin/$APP_NAME" >&2
    printf '%s\n' "build it first: go build -o \"\$HOME/.local/bin/$APP_NAME\" ./cmd/$APP_NAME" >&2
    exit 1
  fi
}

install_systemd_system() {
  if [ "$(id -u)" -eq 0 ]; then
    printf '%s\n' "do not run this installer with sudo; run it as the target user, and the script will call sudo for systemd installation" >&2
    exit 1
  fi
  service_path="/etc/systemd/system/$SYSTEMD_UNIT"
  tmp_path=$(mktemp)
  group_name=$(id -gn)
  sed \
    -e "s#__HOME__#$HOME#g" \
    -e "s#__USER__#$(id -un)#g" \
    -e "s#__GROUP__#$group_name#g" \
    "$ROOT_DIR/packaging/systemd/$SYSTEMD_UNIT" > "$tmp_path"
  sudo install -m 0644 "$tmp_path" "$service_path"
  rm -f "$tmp_path"
  sudo systemctl daemon-reload
  sudo systemctl enable --now "$SYSTEMD_UNIT"
  printf '%s\n' "installed system service: $service_path"
  printf '%s\n' "status: systemctl status $SYSTEMD_UNIT"
  printf '%s\n' "logs: journalctl -u $SYSTEMD_UNIT -f"
}

install_launchd_user() {
  plist_dir="$HOME/Library/LaunchAgents"
  log_dir="$HOME/Library/Logs/$APP_NAME"
  plist_path="$plist_dir/$LAUNCHD_LABEL.plist"
  mkdir -p "$plist_dir" "$log_dir"
  sed "s#__HOME__#$HOME#g" "$ROOT_DIR/packaging/launchd/$LAUNCHD_LABEL.plist" > "$plist_path"
  launchctl bootout "gui/$(id -u)" "$plist_path" >/dev/null 2>&1 || true
  launchctl bootstrap "gui/$(id -u)" "$plist_path"
  launchctl enable "gui/$(id -u)/$LAUNCHD_LABEL"
  launchctl kickstart -k "gui/$(id -u)/$LAUNCHD_LABEL"
  printf '%s\n' "installed launchd agent: $plist_path"
  printf '%s\n' "status: launchctl print gui/$(id -u)/$LAUNCHD_LABEL"
  printf '%s\n' "logs: tail -f \"$log_dir/stderr.log\""
}

main() {
  require_binary
  case "$(uname -s)" in
    Darwin)
      install_launchd_user
      ;;
    Linux)
      if [ -r /etc/debian_version ]; then
        install_systemd_system
      else
        printf '%s\n' "unsupported Linux distribution: this installer currently supports Debian-style systemd services" >&2
        exit 1
      fi
      ;;
    *)
      printf '%s\n' "unsupported OS: $(uname -s)" >&2
      exit 1
      ;;
  esac
}

main "$@"
