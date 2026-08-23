export function cn(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

export function formatBytes(bytes: number): string {
  if (bytes <= 0) return "0 Б";
  const units = ["Б", "КБ", "МБ", "ГБ", "ТБ"];
  const exponent = Math.min(units.length - 1, Math.floor(Math.log(bytes) / Math.log(1024)));
  const value = bytes / Math.pow(1024, exponent);
  const rounded = value >= 100 || exponent === 0 ? Math.round(value) : Math.round(value * 10) / 10;
  return `${rounded.toLocaleString("ru-RU")} ${units[exponent]}`;
}

export function formatCount(value: number): string {
  return value.toLocaleString("ru-RU");
}

export function formatNewsDate(unixNanos: number): string {
  if (!unixNanos) return "";
  return new Date(unixNanos / 1_000_000).toLocaleDateString("ru-RU", { day: "numeric", month: "long" });
}
