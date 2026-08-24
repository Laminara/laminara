export type Phase = "connecting" | "updating" | "login" | "home" | "syncing" | "running";

export type Loader = "vanilla" | "fabric" | "quilt" | "forge" | "neoforge";

export type InstallState = "installed" | "outdated" | "missing" | "syncing";

export interface EndpointStatus {
  id: string;
  baseUrl: string;
  healthy: boolean;
  latencyMs: number | null;
  isCurrent: boolean;
}

export interface Account {
  uuid: string;
  name: string;
  endpointId: string;
}

export interface AuthStatus {
  signedIn: boolean;
  username?: string;
  uuid?: string;
}

export interface Build {
  name: string;
  version: string;
  loader?: Loader;
  sizeBytes: number;
  install: InstallState;
  hasFeatures?: boolean;
  available?: boolean;
  platforms?: string[];
  progress?: number;
  locked?: boolean;
  lockReason?: string;
}

export interface ServerPlayers {
  online: number;
  max: number;
}

export interface PlayerCounts {
  perBuild: Record<string, ServerPlayers>;
  total: ServerPlayers;
}

export interface GeneralSettings {
  installDir: string;
  defaultMemoryMb: number;
  endpoints: { id: string; baseUrl: string }[];
  version: string;
}

export interface BuildSettings {
  maxMemoryMb: number | null;
  defaultMemoryMb: number;
}

export interface FeatureMeta {
  icon: string;
  badge: string;
  addedSize: number;
  requires: string[];
  incompatibleWith: string[];
}

export interface FeatureOption {
  id: string;
  title: string;
  description: string;
  defaultEnabled: boolean;
  files: string[];
  groups: FeatureGroup[];
  meta: FeatureMeta;
}

export interface FeatureGroup {
  id: string;
  title: string;
  description: string;
  selection: "single" | "multi";
  required: boolean;
  options: FeatureOption[];
}

export interface FeatureSelection {
  selected: Record<string, string[]>;
}

export interface BuildFeatures {
  model: FeatureGroup[];
  selection: FeatureSelection;
  active: string[];
}

export interface NewsItem {
  id: string;
  title: string;
  body: string;
  publishedAtUnixNanos: number;
  tag?: string;
  link?: string;
  bannerDataUri?: string;
}

export type ActiveModal =
  | { kind: "general" }
  | { kind: "news" }
  | { kind: "build"; profile: string }
  | { kind: "features"; profile: string }
  | { kind: "library" }
  | null;

export interface SyncState {
  stage: string;
  filesDone: number;
  filesTotal: number;
  bytesDone: number;
  bytesTotal: number;
  currentPath?: string;
}

export type SyncEvent =
  | { event: "started"; data: { filesTotal: number; bytesTotal: number } }
  | {
      event: "progress";
      data: { stage: string; filesDone: number; filesTotal: number; bytesDone: number; bytesTotal: number; currentPath?: string };
    }
  | { event: "finished"; data: { downloaded: number; skipped: number; pruned: number } }
  | { event: "failed"; data: { message: string } };

export interface LauncherUpdate {
  version: string;
  notes: string;
  mandatory: boolean;
  size: number;
  fileName: string;
  canInstall: boolean;
  blockedReason: string | null;
}
