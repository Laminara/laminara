import { CheckSquare, Circle, RadioButton, Square } from "@phosphor-icons/react";
import type { FeatureGroup } from "@/lib/types";
import { blockedReason, effective, type Selected } from "@/lib/features";
import { cn } from "@/lib/format";

interface Props {
  group: FeatureGroup;
  addr: string;
  selected: Selected;
  active: Set<string>;
  onChange: (addr: string, ids: string[]) => void;
  depth?: number;
}

export function FeatureGroupView({ group, addr, selected, active, onChange, depth = 0 }: Props) {
  const chosen = effective(group, addr, selected);
  const single = group.selection === "single";

  const pick = (optionId: string, active: boolean) => {
    if (single) {
      onChange(addr, [optionId]);
    } else {
      onChange(addr, active ? chosen.filter((id) => id !== optionId) : [...chosen, optionId]);
    }
  };

  return (
    <section className={cn(depth > 0 && "mt-3 border-l border-border pl-4")}>
      <div className="mb-2 flex items-baseline gap-2">
        <h3 className="text-sm font-semibold">{group.title}</h3>
        {group.description && <span className="text-xs text-mute">{group.description}</span>}
      </div>

      <div className="flex flex-col gap-1.5">
        {single && !group.required && (
          <Row active={!group.options.some((option) => active.has(`${addr}#${option.id}`))} single onClick={() => onChange(addr, [])} title="Нет" />
        )}

        {group.options.map((option) => {
          const optionAddr = `${addr}#${option.id}`;
          const isActive = active.has(optionAddr);
          const blocked = isActive ? null : blockedReason(option, active);
          return (
            <div key={option.id}>
              <Row
                active={isActive}
                single={single}
                blocked={blocked}
                onClick={() => !blocked && pick(option.id, isActive)}
                title={option.title}
                description={option.description}
                badge={option.meta.badge}
              />
              {isActive &&
                option.groups.map((sub) => (
                  <FeatureGroupView
                    key={sub.id}
                    group={sub}
                    addr={`${optionAddr}/${sub.id}`}
                    selected={selected}
                    active={active}
                    onChange={onChange}
                    depth={depth + 1}
                  />
                ))}
            </div>
          );
        })}
      </div>
    </section>
  );
}

interface RowProps {
  active: boolean;
  single: boolean;
  blocked?: string | null;
  onClick: () => void;
  title: string;
  description?: string;
  badge?: string;
}

function Row({ active, single, blocked, onClick, title, description, badge }: RowProps) {
  const Icon = single ? (active ? RadioButton : Circle) : active ? CheckSquare : Square;
  return (
    <button
      onClick={onClick}
      disabled={Boolean(blocked)}
      className={cn(
        "flex w-full items-center gap-3 rounded-md border px-3 py-2.5 text-left transition-colors",
        active ? "border-primary/50 bg-primary-soft" : "border-border bg-surface",
        blocked ? "cursor-not-allowed opacity-45" : "hover:bg-surface-2",
      )}
    >
      <Icon size={18} weight={active ? "fill" : "regular"} className={active ? "text-primary" : "text-mute"} />
      <span className="flex-1">
        <span className="flex items-center gap-2">
          <span className="text-sm font-medium">{title}</span>
          {badge && <span className="rounded-full bg-primary-soft px-2 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-primary">{badge}</span>}
          {blocked && <span className="rounded-full bg-surface-2 px-2 py-0.5 text-[10px] uppercase tracking-wide text-mute">{blocked}</span>}
        </span>
        {description && <span className="mt-0.5 block text-xs text-mute">{description}</span>}
      </span>
    </button>
  );
}
