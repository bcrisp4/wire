// Vitest unit test for the `swipe` attachment.
//
// jsdom doesn't ship a `PointerEvent` constructor, so we synthesise one by
// extending `MouseEvent` (which it does ship). The attachment listens for
// real DOM `pointerdown`/`pointerup` events; as long as `event.clientX` and
// `event.clientY` are set, the threshold check fires.
//
// We invoke the attachment factory directly against a real DOM node rather
// than mounting a Svelte template. This keeps the test focused on the
// gesture logic and exercises the same `(node) => cleanup` contract Svelte
// uses internally for `{@attach swipe(...)}`.

import { afterEach, beforeEach, describe, expect, test, vi } from 'vitest';
import { swipe } from './swipe';

class FakePointerEvent extends MouseEvent {
	constructor(type: string, init: { clientX: number; clientY: number }) {
		super(type, { bubbles: true, cancelable: true, ...init });
	}
}

function fire(node: HTMLElement, type: 'pointerdown' | 'pointerup', x: number, y: number) {
	node.dispatchEvent(new FakePointerEvent(type, { clientX: x, clientY: y }));
}

describe('swipe attachment', () => {
	let node: HTMLElement;

	beforeEach(() => {
		node = document.createElement('div');
		document.body.appendChild(node);
	});

	afterEach(() => {
		node.remove();
	});

	test('fires onLeft when the pointer moves left past the threshold', () => {
		const onLeft = vi.fn();
		const onRight = vi.fn();
		const cleanup = swipe({ onLeft, onRight })(node);

		fire(node, 'pointerdown', 200, 100);
		fire(node, 'pointerup', 100, 100); // dx = -100

		expect(onLeft).toHaveBeenCalledTimes(1);
		expect(onRight).not.toHaveBeenCalled();

		cleanup?.();
	});

	test('fires onRight when the pointer moves right past the threshold', () => {
		const onLeft = vi.fn();
		const onRight = vi.fn();
		const cleanup = swipe({ onLeft, onRight })(node);

		fire(node, 'pointerdown', 100, 100);
		fire(node, 'pointerup', 200, 100); // dx = +100

		expect(onRight).toHaveBeenCalledTimes(1);
		expect(onLeft).not.toHaveBeenCalled();

		cleanup?.();
	});

	test('does nothing when horizontal delta is below the threshold', () => {
		const onLeft = vi.fn();
		const onRight = vi.fn();
		const cleanup = swipe({ onLeft, onRight })(node);

		fire(node, 'pointerdown', 100, 100);
		fire(node, 'pointerup', 130, 100); // dx = +30

		expect(onLeft).not.toHaveBeenCalled();
		expect(onRight).not.toHaveBeenCalled();

		cleanup?.();
	});

	test('ignores the gesture when vertical delta dominates (scroll)', () => {
		const onLeft = vi.fn();
		const onRight = vi.fn();
		const cleanup = swipe({ onLeft, onRight })(node);

		fire(node, 'pointerdown', 100, 100);
		fire(node, 'pointerup', 160, 200); // dx = 60, dy = 100 → vertical wins

		expect(onLeft).not.toHaveBeenCalled();
		expect(onRight).not.toHaveBeenCalled();

		cleanup?.();
	});

	test('cleanup detaches listeners so later events do not fire handlers', () => {
		const onLeft = vi.fn();
		const onRight = vi.fn();
		const cleanup = swipe({ onLeft, onRight })(node);

		cleanup?.();

		fire(node, 'pointerdown', 200, 100);
		fire(node, 'pointerup', 100, 100);

		expect(onLeft).not.toHaveBeenCalled();
		expect(onRight).not.toHaveBeenCalled();
	});
});
