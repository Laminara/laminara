interface MemoryFieldProps {
  valueMb: number;
  onChange: (mb: number) => void;
  min?: number;
  max?: number;
}

function gbLabel(mb: number): string {
  const gb = mb / 1024;
  return `${Number.isInteger(gb) ? gb : gb.toFixed(1)} ГБ`;
}

export function MemoryField({ valueMb, onChange, min = 1024, max = 16384 }: MemoryFieldProps) {
  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <span className="text-sm text-dim">Оперативная память</span>
        <span className="text-sm font-semibold tabular-nums text-text">{gbLabel(valueMb)}</span>
      </div>
      <input
        type="range"
        min={min}
        max={max}
        step={512}
        value={valueMb}
        onChange={(event) => onChange(Number(event.target.value))}
        className="w-full accent-[var(--lm-primary)]"
      />
      <div className="mt-1 flex justify-between text-[11px] text-mute">
        <span>{gbLabel(min)}</span>
        <span>{gbLabel(max)}</span>
      </div>
    </div>
  );
}
