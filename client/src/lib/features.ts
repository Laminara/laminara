import type { FeatureGroup, FeatureOption } from "@/lib/types";

export type Selected = Record<string, string[]>;

interface ActiveOption {
  order: number;
  requires: string[];
  incompatibleWith: string[];
}

export function defaults(group: FeatureGroup): string[] {
  if (group.selection === "single") {
    const preset = group.options.find((option) => option.defaultEnabled);
    if (preset) return [preset.id];
    if (group.required && group.options[0]) return [group.options[0].id];
    return [];
  }
  return group.options.filter((option) => option.defaultEnabled).map((option) => option.id);
}

export function activeAddresses(groups: FeatureGroup[], selected: Selected): Set<string> {
  const active = new Map<string, ActiveOption>();
  let order = 0;

  const walk = (list: FeatureGroup[], parentAddr: string) => {
    for (const group of list) {
      const groupAddr = parentAddr ? `${parentAddr}/${group.id}` : group.id;
      const chosen = effective(group, groupAddr, selected);
      for (const option of group.options) {
        if (!chosen.includes(option.id)) continue;
        const optionAddr = `${groupAddr}#${option.id}`;
        active.set(optionAddr, {
          order: order++,
          requires: option.meta.requires,
          incompatibleWith: option.meta.incompatibleWith,
        });
        walk(option.groups, optionAddr);
      }
    }
  };
  walk(groups, "");
  enforceConstraints(active);
  return new Set(active.keys());
}

function enforceConstraints(active: Map<string, ActiveOption>) {
  const limit = active.size + 1;
  for (let pass = 0; pass < limit; pass += 1) {
    const victim = firstViolation(active);
    if (!victim) return;
    const prefix = `${victim}/`;
    for (const addr of [...active.keys()]) {
      if (addr === victim || addr.startsWith(prefix)) active.delete(addr);
    }
  }
}

function firstViolation(active: Map<string, ActiveOption>): string | null {
  const ordered = [...active.entries()].sort((a, b) => a[1].order - b[1].order);
  for (const [addr, option] of ordered) {
    if (option.requires.some((needed) => !active.has(needed))) return addr;
  }
  for (let index = 0; index < ordered.length; index += 1) {
    const [addr, option] = ordered[index];
    for (let earlier = 0; earlier < index; earlier += 1) {
      const [otherAddr, other] = ordered[earlier];
      if (option.incompatibleWith.includes(otherAddr) || other.incompatibleWith.includes(addr)) return addr;
    }
  }
  return null;
}

export function blockedReason(option: FeatureOption, active: Set<string>): string | null {
  if (option.meta.requires.some((addr) => !active.has(addr))) return "Требует другую опцию";
  const conflict = option.meta.incompatibleWith.some((addr) => active.has(addr));
  return conflict ? "Несовместимо с выбранным" : null;
}

export function effective(group: FeatureGroup, addr: string, selected: Selected): string[] {
  const saved = selected[addr];
  if (saved === undefined) return defaults(group);
  let ids = group.options.map((option) => option.id).filter((id) => saved.includes(id));
  if (group.selection === "single") {
    ids = ids.slice(0, 1);
    if (ids.length === 0 && group.required) {
      const fallback = group.options.find((option) => option.defaultEnabled) ?? group.options[0];
      if (fallback) ids = [fallback.id];
    }
  }
  return ids;
}
