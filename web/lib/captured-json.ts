import { isSafeNumber, LosslessNumber, parse, stringify } from "lossless-json";

/** Keep ordinary numbers ergonomic while preserving captured numeric literals beyond JS precision. */
export function parseCapturedJSON(text: string): unknown {
  return parse(text, null, {
    parseNumber: (value) =>
      isSafeNumber(value) ? Number(value) : new LosslessNumber(value),
  });
}

export function stringifyCapturedJSON(value: unknown): string {
  return stringify(value, null, 2) ?? "";
}
