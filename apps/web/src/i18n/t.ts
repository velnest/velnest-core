import id from './id.json';
import en from './en.json';

export const locales = ['id', 'en'] as const;
export type Locale = (typeof locales)[number];
export const defaultLocale: Locale = 'id';

const catalogs = { id, en } satisfies Record<Locale, Record<keyof typeof id, string>>;

export type MessageKey = keyof typeof id;

export function isLocale(value: unknown): value is Locale {
  return typeof value === 'string' && (locales as readonly string[]).includes(value);
}

export function t(locale: Locale, key: MessageKey): string {
  return catalogs[locale][key];
}
