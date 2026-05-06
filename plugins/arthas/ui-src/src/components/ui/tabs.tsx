import { createContext } from "preact";
import { useContext, useState } from "preact/hooks";

const TabsContext = createContext<{ value: string; setValue: (v: string) => void } | null>(null);

export function Tabs({
	value,
	defaultValue,
	onValueChange,
	className = "",
	children,
}: {
	value?: string;
	defaultValue?: string;
	onValueChange?: (v: string) => void;
	className?: string;
	children: preact.ComponentChildren;
}) {
	const [internalValue, setInternalValue] = useState(defaultValue ?? "");
	const current = value ?? internalValue;
	const setValue = (v: string) => {
		setInternalValue(v);
		onValueChange?.(v);
	};
	return (
		<TabsContext.Provider value={{ value: current, setValue }}>
			<div className={className}>{children}</div>
		</TabsContext.Provider>
	);
}

export function TabsList({ className = "", children }: { className?: string; children: preact.ComponentChildren }) {
  return <div className={`inline-flex items-center gap-1 rounded-md bg-muted p-1 ${className}`}>{children}</div>;
}

export function TabsTrigger({
  value,
  className = "",
  children,
}: {
  value: string;
  className?: string;
  children: preact.ComponentChildren;
}) {
  const ctx = useContext(TabsContext);
  const active = ctx?.value === value;
  return (
    <button
      type="button"
      onClick={() => ctx?.setValue(value)}
      className={`rounded px-3 py-1 text-sm font-medium ${active ? "bg-background text-foreground shadow" : "text-muted-foreground hover:text-foreground"} ${className}`}
    >
      {children}
    </button>
  );
}

export function TabsContent({
  value,
  className = "",
  children,
}: {
  value: string;
  className?: string;
  children: preact.ComponentChildren;
}) {
  const ctx = useContext(TabsContext);
  if (ctx?.value !== value) return null;
  return <div className={className}>{children}</div>;
}
