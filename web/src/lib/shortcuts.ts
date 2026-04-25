// Global keyboard shortcuts (Unit 19b).
//
// Wired from `+layout.svelte` via `<svelte:window onkeydown={handleKeydown} />`.
// Keys:
//   j  -> focus / scroll to the next entry card
//   k  -> previous entry card
//   m  -> toggle read state of the focused entry
//   s  -> toggle saved state of the focused entry
//   v  -> open the focused entry's URL in a new tab (skips if URL is null)
//   o  -> SvelteKit-navigate to /entry/<id> for the focused entry
//
// The handler is a no-op while the user is typing in an input/textarea/select
// or contenteditable element, and while a modifier key is held — those keys
// remain available for the browser, the OS, and form fields.
//
// Entry cards are located via `[data-entry-id]` on the card's outer element.
// As of writing, `EntryCard.svelte` (Unit 12a) does not yet emit the markers
// this handler reads, which is why the card-locator function is conservative:
// if no element matches, j/k/m/s/v/o quietly do nothing. A small follow-up
// PR will add the following markers to the card's outer `<article>` so this
// handler can find them and read per-card state:
//   - `data-entry-id={entry.id}`           — required to locate the card
//   - `data-entry-url={entry.url}`         — read by `v` (open in new tab)
//   - `class:read={entry.read}`            — already present; read by `m`
//   - `class:saved={entry.saved}` or
//     `data-entry-saved={entry.saved}`     — read by `s`; until one of these
//                                            ships, `s` will always send
//                                            `{saved: true}` (never unsave).
// `o` additionally depends on a `/entry/[id]` route, which is a separate
// follow-up unit; until it ships, `o` will navigate to a 404.

import { goto } from '$app/navigation';
import { api } from '$lib/api';

// Active "current index" across calls so j/k can advance even when no element
// is focused yet. Persists across blur events so that after `j` selects a
// card and the user clicks elsewhere, m/s/v/o still target the j-tracked
// card. Initial value `-1` means "no prior position" — the first `j` then
// starts at the top, the first `k` at the bottom.
let currentIndex = -1;

// Test-only hook: reset module-level state between Vitest cases. Production
// callers don't need this — the index is per-tab and follows the page.
export function _resetForTests(): void {
	currentIndex = -1;
}

function isEditableTarget(target: EventTarget | null): boolean {
	if (!(target instanceof HTMLElement)) return false;
	const tag = target.tagName;
	if (tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT') return true;
	// `isContentEditable` walks up to find inherited contenteditable. jsdom
	// doesn't implement it, so we also accept the literal attribute as a
	// fallback for a `[contenteditable=true]` ancestor lookup.
	if (target.isContentEditable) return true;
	if (target.closest('[contenteditable="true"]') !== null) return true;
	return false;
}

function listCards(): HTMLElement[] {
	if (typeof document === 'undefined') return [];
	return Array.from(document.querySelectorAll<HTMLElement>('[data-entry-id]'));
}

// Finds the "current" card, preferring an ancestor of the active element so
// that focus is the source of truth. Falls back to the cached index from a
// previous j/k. Returns null if nothing matches.
function focusedCard(): HTMLElement | null {
	if (typeof document === 'undefined') return null;
	const active = document.activeElement;
	if (active instanceof HTMLElement) {
		const ancestor = active.closest<HTMLElement>('[data-entry-id]');
		if (ancestor) return ancestor;
	}
	const cards = listCards();
	if (cards.length === 0) return null;
	if (currentIndex >= 0 && currentIndex < cards.length) return cards[currentIndex];
	return null;
}

function focusCard(card: HTMLElement) {
	// If the card itself isn't focusable, give it `tabindex=-1` so .focus()
	// works without making it part of normal tab order. Then prefer the most
	// "interactive" inner control (the title button) so Enter/Space behave.
	if (!card.hasAttribute('tabindex')) card.setAttribute('tabindex', '-1');
	const inner = card.querySelector<HTMLElement>('button, a, [tabindex]');
	(inner ?? card).focus();
	// `scrollIntoView` is missing in jsdom, hence the typeof guard. In real
	// browsers this keeps the focused card visible without yanking the page.
	if (typeof card.scrollIntoView === 'function') {
		card.scrollIntoView({ block: 'nearest', behavior: 'smooth' });
	}
}

function moveFocus(delta: 1 | -1) {
	const cards = listCards();
	if (cards.length === 0) return;
	const current = focusedCard();
	let idx = current ? cards.indexOf(current) : -1;
	if (idx === -1) {
		// No prior position — j starts at 0, k starts at the last card.
		idx = delta === 1 ? 0 : cards.length - 1;
	} else {
		idx = Math.min(cards.length - 1, Math.max(0, idx + delta));
	}
	currentIndex = idx;
	focusCard(cards[idx]);
}

function entryIDFromCard(card: HTMLElement): number | null {
	const raw = card.getAttribute('data-entry-id');
	if (!raw) return null;
	const n = Number(raw);
	if (!Number.isFinite(n) || n <= 0) return null;
	return n;
}

async function patchEntry(card: HTMLElement, body: { read?: boolean; saved?: boolean }) {
	const id = entryIDFromCard(card);
	if (id === null) return;
	try {
		await api.put(`/entries/${id}`, body);
	} catch (err) {
		// No toast UI here yet; log so the failure is visible in devtools and
		// not silently swallowed.
		console.warn('shortcuts: PUT /entries failed', err);
	}
}

// Per-card actions for keys that operate on the focused entry. Using a table
// keeps the switch in `handleKeydown` short and makes adding a new key a
// one-line addition rather than another four-line case block.
const cardActions: Record<string, (card: HTMLElement) => void> = {
	m: (card) => void patchEntry(card, { read: !card.classList.contains('read') }),
	s: (card) => {
		// Accept either signal so the follow-up PR can pick whichever fits the
		// card markup: a `class:saved` toggle or an explicit data attribute.
		const isSaved = card.classList.contains('saved') || card.getAttribute('data-entry-saved') === 'true';
		void patchEntry(card, { saved: !isSaved });
	},
	v: (card) => {
		const url = card.getAttribute('data-entry-url');
		if (!url) return;
		// `noopener,noreferrer` matches the EntryCard "View original" link.
		window.open(url, '_blank', 'noopener,noreferrer');
	},
	o: (card) => {
		const id = entryIDFromCard(card);
		if (id !== null) void goto(`/entry/${id}`);
	}
};

export function handleKeydown(event: KeyboardEvent): void {
	if (isEditableTarget(event.target)) return;
	if (event.ctrlKey || event.metaKey || event.altKey) return;

	if (event.key === 'j') {
		event.preventDefault();
		moveFocus(1);
		return;
	}
	if (event.key === 'k') {
		event.preventDefault();
		moveFocus(-1);
		return;
	}
	const action = cardActions[event.key];
	if (!action) return;
	const card = focusedCard();
	if (!card) return;
	event.preventDefault();
	action(card);
}
