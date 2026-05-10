import { ConfigIcon } from "../../ConfigIcon";

export function Icon({
  name,
  size = 14,
  className,
}: {
  name?: string;
  size?: number;
  className?: string;
}) {
  return (
    <ConfigIcon
      primary={name}
      className={className ?? "inline-block"}
      size={size}
    />
  );
}
