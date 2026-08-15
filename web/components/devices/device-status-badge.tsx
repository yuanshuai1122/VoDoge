"use client";

import { cn } from "@/lib/utils";
import {
  summarizeDeviceStatus,
  signalLevel,
  TONE_CLASS,
} from "@/lib/device-status";
import type { DeviceOverview } from "@/types/device";
import { useLocale } from "@/lib/i18n";

export function DeviceStatusBadge({
  device,
  showDetail = false,
}: {
  device: DeviceOverview;
  showDetail?: boolean;
}) {
  useLocale();
  const status = summarizeDeviceStatus(device);

  return (
    <div className="flex flex-col gap-0.5">
      <span
        title={status.detail}
        className={cn(
          "inline-flex w-fit items-center gap-1.5 rounded-full px-2 py-0.5 text-xs font-medium",
          TONE_CLASS[status.tone],
        )}
      >
        <span
          className={cn(
            "size-1.5 rounded-full bg-current",
            status.transient && "animate-pulse",
          )}
        />
        {status.label}
      </span>
      {showDetail && (
        <span className="text-xs text-muted-foreground">{status.detail}</span>
      )}
    </div>
  );
}

export function SignalIndicator({ rsrp }: { rsrp: number | undefined }) {
  useLocale();
  const level = signalLevel(rsrp);

  return (
    <span className="inline-flex items-center gap-1.5 text-xs">
      <span
        className={cn(
          "rounded px-1.5 py-0.5 font-medium",
          TONE_CLASS[level.tone],
        )}
      >
        {level.label}
      </span>
      {rsrp ? (
        <span className="tabular-nums text-muted-foreground">{rsrp} dBm</span>
      ) : null}
    </span>
  );
}
