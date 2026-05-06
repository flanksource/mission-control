import type { ComponentChildren } from "preact";
import type { ButtonHTMLAttributes } from "react";

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: "default" | "ghost" | "outline" | "secondary" | "destructive";
  size?: "sm" | "xs" | "default";
  loading?: boolean;
};

export function Button({
  className = "",
  variant = "default",
  size = "default",
  loading,
  disabled,
  children,
  ...props
}: ButtonProps) {
  const variantClass =
    variant === "ghost"
      ? "border-transparent bg-transparent hover:bg-muted"
      : variant === "outline"
        ? "border-border bg-background hover:bg-muted"
        : variant === "secondary"
          ? "border-transparent bg-secondary text-secondary-foreground hover:bg-secondary/80"
          : variant === "destructive"
            ? "border-red-600 bg-red-600 text-white hover:bg-red-700"
            : "border-primary bg-primary text-primary-foreground hover:bg-primary/90";
  const sizeClass = size === "xs" ? "h-6 px-2 text-xs" : size === "sm" ? "h-7 px-2 text-xs" : "h-8 px-3 text-sm";
  return (
    <button
      type="button"
      className={`inline-flex items-center justify-center gap-1 rounded-md border font-medium disabled:pointer-events-none disabled:opacity-60 ${variantClass} ${sizeClass} ${className}`}
      disabled={disabled || loading}
      {...props}
    >
      {loading ? <span className="h-3 w-3 animate-spin rounded-full border-2 border-current border-r-transparent" /> : children}
    </button>
  );
}

export type { ComponentChildren };
