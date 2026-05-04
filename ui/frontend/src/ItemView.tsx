import type { ClickyCommandRuntime } from "@flanksource/clicky-ui";
import { ConfigItemDetail } from "./config-detail/ConfigItemDetail";

export type ItemViewProps = {
  id: string;
  commandRuntime: ClickyCommandRuntime;
};

export function ItemView({ id, commandRuntime: _commandRuntime }: ItemViewProps) {
  return <ConfigItemDetail id={id} />;
}
