// Vitest unit tests for the prefs module. Covers:
//  - getTheme/getFont default to 'system'/'sans' when no value is stored.
//  - Invalid stored values are rejected and fall back to defaults.
//  - setTheme('system') *removes* the data-theme attribute (does not set it
//    to "system"), which is how CSS picks up the OS-level preference.
//  - setTheme/setFont write to localStorage and reflect onto
//    document.documentElement.dataset.
//  - readStorage / writeStorage tolerate localStorage throwing (private mode,
//    quota) without breaking the page.

import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { getTheme, getFont, setTheme, setFont, applyStoredPrefs } from './prefs';

const THEME_KEY = 'wire.theme';
const FONT_KEY = 'wire.font';

describe('prefs', () => {
	beforeEach(() => {
		localStorage.clear();
		delete document.documentElement.dataset.theme;
		delete document.documentElement.dataset.font;
	});

	afterEach(() => {
		vi.restoreAllMocks();
	});

	test('getTheme returns "system" when nothing is stored', () => {
		expect(getTheme()).toBe('system');
	});

	test('getTheme returns "system" for an invalid stored value', () => {
		localStorage.setItem(THEME_KEY, 'neon');
		expect(getTheme()).toBe('system');
	});

	test('getTheme returns the stored value when valid', () => {
		localStorage.setItem(THEME_KEY, 'dark');
		expect(getTheme()).toBe('dark');
	});

	test('getFont returns "sans" when nothing is stored', () => {
		expect(getFont()).toBe('sans');
	});

	test('getFont returns "sans" for an invalid stored value', () => {
		localStorage.setItem(FONT_KEY, 'comic');
		expect(getFont()).toBe('sans');
	});

	test('getFont returns the stored value when valid', () => {
		localStorage.setItem(FONT_KEY, 'serif');
		expect(getFont()).toBe('serif');
	});

	test('setTheme persists and applies a non-system theme to dataset', () => {
		setTheme('dark');
		expect(localStorage.getItem(THEME_KEY)).toBe('dark');
		expect(document.documentElement.dataset.theme).toBe('dark');
	});

	test('setTheme("system") removes data-theme rather than setting it', () => {
		document.documentElement.dataset.theme = 'dark';
		setTheme('system');
		expect(localStorage.getItem(THEME_KEY)).toBe('system');
		expect(document.documentElement.dataset.theme).toBeUndefined();
	});

	test('setFont persists and applies the font to dataset', () => {
		setFont('serif');
		expect(localStorage.getItem(FONT_KEY)).toBe('serif');
		expect(document.documentElement.dataset.font).toBe('serif');
	});

	test('setTheme tolerates localStorage.setItem throwing', () => {
		const spy = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
			throw new Error('quota exceeded');
		});
		expect(() => setTheme('sepia')).not.toThrow();
		// Dataset still applied even when persistence fails.
		expect(document.documentElement.dataset.theme).toBe('sepia');
		spy.mockRestore();
	});

	test('getTheme tolerates localStorage.getItem throwing', () => {
		const spy = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
			throw new Error('disabled');
		});
		expect(getTheme()).toBe('system');
		spy.mockRestore();
	});

	test('applyStoredPrefs reflects stored theme + font onto dataset', () => {
		localStorage.setItem(THEME_KEY, 'sepia');
		localStorage.setItem(FONT_KEY, 'serif');
		applyStoredPrefs();
		expect(document.documentElement.dataset.theme).toBe('sepia');
		expect(document.documentElement.dataset.font).toBe('serif');
	});

	test('applyStoredPrefs with no stored values clears theme and sets default font', () => {
		document.documentElement.dataset.theme = 'dark';
		applyStoredPrefs();
		// Default theme is 'system' → data-theme removed.
		expect(document.documentElement.dataset.theme).toBeUndefined();
		// Default font is 'sans' → applied.
		expect(document.documentElement.dataset.font).toBe('sans');
	});
});
