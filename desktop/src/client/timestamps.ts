/** The server uses Unix seconds; older clients may retain millisecond values. */
export function timestampMillis(value: unknown, fallback = 0): number {
  if (typeof value !== "number" || !Number.isFinite(value) || value <= 0)
    return fallback;
  return value < 1e12 ? value * 1000 : value;
}
