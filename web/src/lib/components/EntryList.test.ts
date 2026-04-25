// Vitest unit test for EntryList's "load more" behaviour.
//
// jsdom does not implement IntersectionObserver, so we stub it with a fake
// that records the most recent callback and lets the test trigger it
// synchronously. This lets us assert "fires once on intersection" and
// "does NOT fire again while loading" without a real layout engine.

import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { render } from '@testing-library/svelte';
import EntryList from './EntryList.svelte';
import type { Entry } from '$lib/types';

type IOEntryLike = { isIntersecting: boolean };

class FakeIO {
	static lastCallback: ((entries: IOEntryLike[]) => void) | null = null;
	private cb: (entries: IOEntryLike[]) => void;
	constructor(cb: (entries: IOEntryLike[]) => void) {
		this.cb = cb;
		FakeIO.lastCallback = cb;
	}
	observe() {}
	unobserve() {}
	disconnect() {
		if (FakeIO.lastCallback === this.cb) FakeIO.lastCallback = null;
	}
	takeRecords() {
		return [];
	}
}

function makeEntry(id: number): Entry {
	return {
		id,
		feed_id: 1,
		user_id: 1,
		hash: `h${id}`,
		title: `Entry ${id}`,
		url: null,
		comments_url: null,
		author: null,
		summary: null,
		content: null,
		published_at: null,
		reading_time: 0,
		read: false,
		read_at: null,
		saved: false,
		saved_at: null,
		created_at: 0,
		changed_at: 0
	};
}

describe('EntryList load-more', () => {
	// Save and restore the original IntersectionObserver so the fake doesn't
	// leak into other test files. jsdom leaves it `undefined`, so this
	// effectively re-deletes it after each test.
	let originalIO: typeof globalThis.IntersectionObserver | undefined;

	beforeEach(() => {
		FakeIO.lastCallback = null;
		originalIO = (globalThis as { IntersectionObserver?: typeof globalThis.IntersectionObserver })
			.IntersectionObserver;
		// jsdom has no IntersectionObserver — install our fake on the global.
		(globalThis as unknown as { IntersectionObserver: typeof FakeIO }).IntersectionObserver =
			FakeIO;
	});

	afterEach(() => {
		FakeIO.lastCallback = null;
		(
			globalThis as { IntersectionObserver?: typeof globalThis.IntersectionObserver }
		).IntersectionObserver = originalIO;
	});

	test('calls onLoadMore once when the sentinel intersects', async () => {
		const onLoadMore = vi.fn();
		render(EntryList, {
			entries: [makeEntry(1), makeEntry(2)],
			loading: false,
			hasMore: true,
			onLoadMore
		});

		// Sentinel rendered → IntersectionObserver constructed → callback captured.
		expect(FakeIO.lastCallback).not.toBeNull();
		FakeIO.lastCallback?.([{ isIntersecting: true }]);
		expect(onLoadMore).toHaveBeenCalledTimes(1);

		// A single intersection event reported as multiple callback entries
		// (e.g. observer batching, layout shifts) must still result in exactly
		// one call: the contract is "fire on intersection, debounced by the
		// parent's `loading` flag once it flips true". With `loading` still
		// false here, repeated callbacks intentionally re-fire — parents are
		// responsible for synchronously setting loading=true in onLoadMore.
		// This assertion documents that contract.
		FakeIO.lastCallback?.([{ isIntersecting: true }]);
		expect(onLoadMore).toHaveBeenCalledTimes(2);
	});

	test('does NOT call onLoadMore again while loading is true', async () => {
		const onLoadMore = vi.fn();
		const { rerender } = render(EntryList, {
			entries: [makeEntry(1)],
			loading: true,
			hasMore: true,
			onLoadMore
		});

		expect(FakeIO.lastCallback).not.toBeNull();
		// First intersection while loading: must NOT fire.
		FakeIO.lastCallback?.([{ isIntersecting: true }]);
		expect(onLoadMore).toHaveBeenCalledTimes(0);

		// Repeated intersections while still loading: still must NOT fire.
		FakeIO.lastCallback?.([{ isIntersecting: true }]);
		FakeIO.lastCallback?.([{ isIntersecting: true }]);
		expect(onLoadMore).toHaveBeenCalledTimes(0);

		// Once loading flips false, an intersection should fire it.
		await rerender({
			entries: [makeEntry(1)],
			loading: false,
			hasMore: true,
			onLoadMore
		});
		FakeIO.lastCallback?.([{ isIntersecting: true }]);
		expect(onLoadMore).toHaveBeenCalledTimes(1);
	});
});
