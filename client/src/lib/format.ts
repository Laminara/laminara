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

export function plural(value: number, one: string, few: string, many: string): string {
  const mod100 = Math.abs(value) % 100;
  const mod10 = mod100 % 10;
  if (mod100 >= 11 && mod100 <= 14) return many;
  if (mod10 === 1) return one;
  if (mod10 >= 2 && mod10 <= 4) return few;
  return many;
}

export function formatNewsDate(unixNanos: number): string {
  if (!unixNanos) return "";
  return new Date(unixNanos / 1_000_000).toLocaleDateString("ru-RU", { day: "numeric", month: "long" });
}

export function formatSpeed(bytesPerSecond: number): string {
  return `${formatBytes(bytesPerSecond)}/с`;
}

export function formatLeft(seconds: number): string {
  if (seconds < 60) return `меньше минуты`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} ${plural(minutes, "минута", "минуты", "минут")}`;
  const hours = Math.floor(minutes / 60);
  return `${hours} ${plural(hours, "час", "часа", "часов")} ${minutes % 60} ${plural(minutes % 60, "минута", "минуты", "минут")}`;
}
