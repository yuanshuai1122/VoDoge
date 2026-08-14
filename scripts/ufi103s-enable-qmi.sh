#!/usr/bin/env bash
# Temporarily expose the stock UFI103S Qualcomm RMNET/QMI USB composition.
#
# This intentionally does not change persist.* properties or any flash partition.
set -euo pipefail

readonly QMI_USB_CONFIG="diag,serial_smd,rmnet_bam,adb"
readonly RNDIS_USB_CONFIG="rndis,serial_smd,adb"
readonly ADB_BIN="${ADB_BIN:-adb}"
readonly WAIT_SECONDS=30

serial=""
cleanup_needed=0
target_config="$QMI_USB_CONFIG"
target_tethering=false
target_name="QMI"

usage() {
	cat <<'EOF'
Usage: scripts/ufi103s-enable-qmi.sh --serial ADB_SERIAL [--restore-rndis]

Temporarily switches one verified UFI103S from its stock RNDIS composition to
the stock RMNET/QMI composition. It does not write firmware, partitions, or
persist.* properties. The change is a staged validation step, not a permanent
factory provisioning method.

Options:
  --restore-rndis  Restore the tested stock RNDIS composition for this build.

Environment:
  ADB_BIN  adb executable to use (default: adb)
EOF
}

die() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

adb_run() {
	"$ADB_BIN" -s "$serial" "$@"
}

wait_for_device() {
	local deadline state
	deadline=$((SECONDS + WAIT_SECONDS))
	while (( SECONDS < deadline )); do
		state="$(adb_run get-state 2>/dev/null || true)"
		state="${state//$'\r'/}"
		if [[ "$state" == "device" ]]; then
			return 0
		fi
		sleep 1
	done
	return 1
}

read_prop() {
	local value
	value="$(adb_run shell getprop "$1")"
	printf '%s' "${value//$'\r'/}"
}

disable_root_adb() {
	if ! wait_for_device; then
		printf 'warning: unable to reach %s to disable root ADB\n' "$serial" >&2
		return 1
	fi
	if ! adb_run shell setprop service.adb.root 0 >/dev/null 2>&1; then
		printf 'warning: unable to request root ADB cleanup for %s\n' "$serial" >&2
		return 1
	fi
	if ! adb_run shell busybox killall adbd >/dev/null 2>&1; then
		printf 'warning: unable to restart adbd for %s during cleanup\n' "$serial" >&2
		return 1
	fi
	return 0
}

cleanup() {
	if [[ "$cleanup_needed" -eq 1 ]]; then
		disable_root_adb || true
	fi
}

while (($# > 0)); do
	case "$1" in
	--serial)
		(($# >= 2)) || die "--serial needs an ADB serial"
		serial="$2"
		shift 2
		;;
	--restore-rndis)
		target_config="$RNDIS_USB_CONFIG"
		target_tethering=true
		target_name="RNDIS"
		shift
		;;
	-h|--help)
		usage
		exit 0
		;;
	*)
		die "unknown argument: $1"
		;;
	esac
done

[[ -n "$serial" ]] || die "--serial is required; do not bulk-switch unverified devices"
command -v "$ADB_BIN" >/dev/null 2>&1 || die "adb not found: $ADB_BIN"
wait_for_device || die "ADB device $serial is not ready"

build_id="$(read_prop ro.build.sw.custom.version)"
[[ "$build_id" == *UFI103S* ]] || die "refusing non-UFI103S build: ${build_id:-unknown}"

trap 'exit 130' INT
trap 'exit 143' TERM
trap cleanup EXIT
cleanup_needed=1

printf 'Enabling temporary root ADB on %s (%s) ...\n' "$serial" "$build_id"
adb_run shell setprop service.adb.root 1
adb_run shell busybox killall adbd
wait_for_device || die "ADB did not return after restarting adbd"

identity="$(adb_run shell id)"
[[ "$identity" == uid=0\(root\)* ]] || die "root ADB is unavailable; refusing to change USB mode"

printf 'Switching %s to %s ...\n' "$serial" "$target_config"
adb_run shell setprop sys.usb.tethering "$target_tethering"
adb_run shell setprop sys.usb.config "$target_config"
wait_for_device || die "ADB did not return after USB re-enumeration"

usb_config="$(read_prop sys.usb.config)"
usb_state="$(read_prop sys.usb.state)"
if [[ "$target_name" == "QMI" ]]; then
	if [[ "$usb_config" != "$target_config" || "$usb_state" != "$target_config" ]]; then
		die "USB mode did not settle: config=$usb_config state=$usb_state"
	fi
elif [[ "$usb_config" != "$target_config" || ( "$usb_state" != "rndis,adb" && "$usb_state" != "$target_config" ) ]]; then
	die "USB mode did not settle: config=$usb_config state=$usb_state"
fi

disable_root_adb || die "unable to disable root ADB after switching USB mode"
wait_for_device || die "ADB did not return after root ADB cleanup"
identity="$(adb_run shell id)"
[[ "$identity" == uid=2000\(shell\)* ]] || die "root ADB cleanup could not be verified"
cleanup_needed=0

if [[ "$target_name" == "QMI" ]]; then
	printf 'QMI candidate mode is active for %s. Verify 05c6:9091, qmi_wwan, cdc-wdm, and wwan on the Debian host.\n' "$serial"
else
	printf 'Stock RNDIS mode is active for %s. Verify 05c6:90b4 before relying on the recovery path.\n' "$serial"
fi
