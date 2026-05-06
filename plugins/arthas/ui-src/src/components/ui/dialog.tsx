import { createContext, cloneElement, isValidElement } from "preact";
import { useContext } from "preact/hooks";
import type { VNode } from "preact";

const DialogContext = createContext<{ close: () => void } | null>(null);

export function Dialog({
  open,
  onOpenChange,
  children,
}: {
  open: boolean;
  onOpenChange?: (open: boolean) => void;
  children: preact.ComponentChildren;
}) {
  if (!open) return null;
  return (
    <DialogContext.Provider value={{ close: () => onOpenChange?.(false) }}>
      <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">{children}</div>
    </DialogContext.Provider>
  );
}

export function DialogPopup({ className = "", children }: { className?: string; children: preact.ComponentChildren }) {
  return <div className={`max-h-[90vh] overflow-auto rounded-md border bg-background p-4 shadow-lg ${className}`}>{children}</div>;
}

export function DialogHeader({ children }: { children: preact.ComponentChildren }) {
  return <div className="mb-3 flex flex-col gap-1">{children}</div>;
}

export function DialogTitle({ children }: { children: preact.ComponentChildren }) {
  return <h2 className="text-base font-semibold">{children}</h2>;
}

export function DialogDescription({ children }: { children: preact.ComponentChildren }) {
  return <p className="text-sm text-muted-foreground">{children}</p>;
}

export function DialogPanel({ children }: { children: preact.ComponentChildren }) {
  return <div className="py-2">{children}</div>;
}

export function DialogFooter({ children }: { children: preact.ComponentChildren }) {
  return <div className="mt-3 flex justify-end gap-2">{children}</div>;
}

export function DialogClose({
  render,
  children,
}: {
  render?: VNode;
  children: preact.ComponentChildren;
}) {
  const ctx = useContext(DialogContext);
  if (render && isValidElement(render)) {
    return cloneElement(render, { onClick: () => ctx?.close(), children });
  }
  return (
    <button type="button" onClick={() => ctx?.close()} className="rounded-md border px-3 py-1 text-sm">
      {children}
    </button>
  );
}
