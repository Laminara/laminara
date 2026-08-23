import type { Build } from "@/lib/types";

export interface BuildBlock {
  badge: string;
  reason: string;
}

const platformBlock: BuildBlock = {
  badge: "Нет для вашей ОС",
  reason: "Эта сборка не собрана для вашей системы",
};

const lockedFallback = "Доступ к этой сборке выдаётся вручную";

export function buildBlock(build: Build | null | undefined): BuildBlock | null {
  if (!build) return null;
  if (build.locked) return { badge: "Нет доступа", reason: build.lockReason?.trim() || lockedFallback };
  if (build.available === false) return platformBlock;
  return null;
}

export function isPlayable(build: Build): boolean {
  return buildBlock(build) === null;
}
