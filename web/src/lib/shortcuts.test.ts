// Vitest unit tests for the global keyboard shortcut handler.
//
// The handler reaches into the live DOM (focus, classes, data-* attributes),
// so each test renders a small fixture into `document.body` rather than
// mounting a Svelte component. This keeps the test focused on the handler's
// branching logic without dragging in a card component the unit doesn't own.

import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';

// `goto` and `api.put` are the only side-effecting collaborators; both are
// mocked at the module boundary so the tests stay deterministic and don't
// depend on a running backend.
const gotoMock = vi.fn();
vi.mock('$app/navigation', () => ({ goto: (...a: unknown[]) => gotoMock(...a) }));

const putMock = vi.fn();
vi.mock('$lib/api', () => ({
	api: {
		put: (...a: unknown[]) => putMock(...a)
	}
}));

import { _resetForTests, handleKeydown } from './shortcuts';

function clearBody() {
	// Move focus off any soon-to-be-orphaned element first; jsdom otherwise
	// keeps `document.activeElement` pointing at the removed button.
	if (document.activeElement instanceof HTMLElement && document.activeElement !== document.body) {
		document.activeElement.blur();
	}
	while (document.body.firstChild) {
		document.body.removeChild(document.body.firstChild);
	}
}

function makeCard(id: number, opts: { read?: boolean; saved?: boolean; url?: string } = {}) {
	const article = document.createElement('article');
	article.setAttribute('data-entry-id', String(id));
	if (opts.url) article.setAttribute('data-entry-url', opts.url);
	if (opts.read) article.classList.add('read');
	if (opts.saved) article.setAttribute('data-entry-saved', 'true');
	// Inner button mirrors EntryCard's structure so focus tests reflect reality.
	const btn = document.createElement('button');
	btn.type = 'button';
	btn.textContent = `Entry ${id}`;
	article.appendChild(btn);
	document.body.appendChild(article);
	return article;
}

function press(key: string, target: EventTarget = document.body, modifiers: Partial<KeyboardEventInit> = {}) {
	const ev = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...modifiers });
	Object.defineProperty(ev, 'target', { value: target, configurable: true });
	handleKeydown(ev);
	return ev;
}

describe('handleKeydown', () => {
	beforeEach(() => {
		clearBody();
		gotoMock.mockReset();
		putMock.mockReset();
		// Explicit focus reset; some prior test may have focused a button that
		// jsdom remembers across mutations even after the element is removed.
		(document.body as HTMLElement).focus();
		_resetForTests();
	});

	afterEach(() => {
		clearBody();
	});

	test('ignores keys when the event target is an input', () => {
		makeCard(1);
		const input = document.createElement('input');
		document.body.appendChild(input);
		press('j', input);
		// No focus shift, no API calls.
		expect(document.activeElement).not.toBeInstanceOf(HTMLButtonElement);
	});

	test('ignores keys when the event target is a textarea', () => {
		makeCard(1);
		const ta = document.createElement('textarea');
		document.body.appendChild(ta);
		press('j', ta);
		expect(document.activeElement).not.toBeInstanceOf(HTMLButtonElement);
	});

	test('ignores keys when target is contenteditable', () => {
		makeCard(1);
		const div = document.createElement('div');
		div.setAttribute('contenteditable', 'true');
		document.body.appendChild(div);
		press('j', div);
		expect(document.activeElement).not.toBeInstanceOf(HTMLButtonElement);
	});

	test('ignores keys when a modifier is held', () => {
		makeCard(1);
		press('j', document.body, { ctrlKey: true });
		expect(document.activeElement).not.toBeInstanceOf(HTMLButtonElement);
		press('j', document.body, { metaKey: true });
		expect(document.activeElement).not.toBeInstanceOf(HTMLButtonElement);
		press('j', document.body, { altKey: true });
		expect(document.activeElement).not.toBeInstanceOf(HTMLButtonElement);
	});

	test('j focuses the first card, then advances; k goes back', () => {
		const a = makeCard(1);
		const b = makeCard(2);
		const c = makeCard(3);

		press('j');
		expect(a.contains(document.activeElement)).toBe(true);

		press('j');
		expect(b.contains(document.activeElement)).toBe(true);

		press('j');
		expect(c.contains(document.activeElement)).toBe(true);

		// Already at last — clamp, don't wrap.
		press('j');
		expect(c.contains(document.activeElement)).toBe(true);

		press('k');
		expect(b.contains(document.activeElement)).toBe(true);

		press('k');
		expect(a.contains(document.activeElement)).toBe(true);

		// Already at first — clamp.
		press('k');
		expect(a.contains(document.activeElement)).toBe(true);
	});

	test('j with no cards is a no-op', () => {
		press('j');
		expect(putMock).not.toHaveBeenCalled();
		expect(gotoMock).not.toHaveBeenCalled();
	});

	test('m toggles read on the focused card', () => {
		const card = makeCard(7, { read: false });
		(card.querySelector('button') as HTMLButtonElement).focus();
		press('m');
		expect(putMock).toHaveBeenCalledWith('/entries/7', { read: true });
	});

	test('m on a read card flips it back to unread', () => {
		const card = makeCard(7, { read: true });
		(card.querySelector('button') as HTMLButtonElement).focus();
		press('m');
		expect(putMock).toHaveBeenCalledWith('/entries/7', { read: false });
	});

	test('s toggles saved on the focused card', () => {
		const card = makeCard(8, { saved: false });
		(card.querySelector('button') as HTMLButtonElement).focus();
		press('s');
		expect(putMock).toHaveBeenCalledWith('/entries/8', { saved: true });
	});

	test('s on an already-saved card flips it back', () => {
		const card = makeCard(8, { saved: true });
		(card.querySelector('button') as HTMLButtonElement).focus();
		press('s');
		expect(putMock).toHaveBeenCalledWith('/entries/8', { saved: false });
	});

	test('s also recognises the saved state via a `saved` class', () => {
		// The follow-up PR may opt to mark saved state with `class:saved` rather
		// than an explicit data attribute; both should round-trip correctly.
		const card = makeCard(8);
		card.classList.add('saved');
		(card.querySelector('button') as HTMLButtonElement).focus();
		press('s');
		expect(putMock).toHaveBeenCalledWith('/entries/8', { saved: false });
	});

	test('v opens entry.url in a new tab when present', () => {
		const card = makeCard(9, { url: 'https://example.com/post' });
		(card.querySelector('button') as HTMLButtonElement).focus();
		const openSpy = vi.spyOn(window, 'open').mockReturnValue(null);
		press('v');
		expect(openSpy).toHaveBeenCalledWith('https://example.com/post', '_blank', 'noopener,noreferrer');
		openSpy.mockRestore();
	});

	test('v with no URL is a no-op', () => {
		const card = makeCard(9);
		(card.querySelector('button') as HTMLButtonElement).focus();
		const openSpy = vi.spyOn(window, 'open').mockReturnValue(null);
		press('v');
		expect(openSpy).not.toHaveBeenCalled();
		openSpy.mockRestore();
	});

	test('o navigates to /entry/<id>', () => {
		const card = makeCard(42);
		(card.querySelector('button') as HTMLButtonElement).focus();
		press('o');
		expect(gotoMock).toHaveBeenCalledWith('/entry/42');
	});

	test('m/s/v/o are no-ops when no card is focused and no current index', () => {
		makeCard(1);
		press('m');
		press('s');
		press('v');
		press('o');
		expect(putMock).not.toHaveBeenCalled();
		expect(gotoMock).not.toHaveBeenCalled();
	});

	test('m/s use the j-tracked card when focus is outside any card', () => {
		makeCard(1);
		makeCard(2);
		press('j'); // currentIndex -> 0
		press('j'); // currentIndex -> 1, focus on card 2's button
		// Move focus elsewhere; cached index should still resolve card 2.
		(document.body as HTMLElement).focus();
		press('m');
		expect(putMock).toHaveBeenCalledWith('/entries/2', { read: true });
	});

	test('unhandled keys do not preventDefault and do not call collaborators', () => {
		makeCard(1);
		const ev = press('x');
		expect(ev.defaultPrevented).toBe(false);
		expect(putMock).not.toHaveBeenCalled();
		expect(gotoMock).not.toHaveBeenCalled();
	});
});
