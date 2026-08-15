import { Badge } from "@/components/ui/badge";
import { laneLabel, normalizeLane } from "@/lib/lane";

export function LaneBadge({
  lane,
  className,
}: {
  lane?: string | null;
  className?: string;
}) {
  const value = normalizeLane(lane);
  const label = laneLabel(value);
  if (!label) return null;
  return (
    <Badge variant="outline" className={className}>
      {label}
    </Badge>
  );
}
