import { cn } from "@/lib/format";

interface SwitchProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  labelledBy?: string;
  describedBy?: string;
}

export function Switch({ checked, onChange, labelledBy, describedBy }: SwitchProps) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={checked}
      aria-labelledby={labelledBy}
      aria-describedby={describedBy}
      onClick={() => onChange(!checked)}
      className={cn(
        "relative inline-flex h-6 w-11 shrink-0 items-center rounded-full border transition-colors focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-primary",
        checked ? "border-primary bg-primary" : "border-border-strong bg-surface-2 hover:bg-surface-3",
      )}
    >
      <span
        className={cn(
          "inline-block h-[18px] w-[18px] rounded-full shadow-sm transition-transform duration-200 ease-out",
          checked ? "translate-x-[21px] bg-primary-ink" : "translate-x-[2px] bg-dim",
        )}
      />
    </button>
  );
}
