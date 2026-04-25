// Svelte 5 attachment factory for left/right swipe gestures.
//
// Usage:
//
//   <div {@attach swipe({ onLeft, onRight })}>...</div>
//
// The attachment listens for pointerdown/pointerup on the host element and
// calls `onLeft` or `onRight` when the user releases at least 50px to the
// left or right of where they pressed. If the vertical delta exceeds the
// horizontal delta the gesture is treated as a scroll and ignored, so this
// won't fight a vertical scroll inside the same element.
//
// Only the first active pointer is tracked. Secondary pointerdowns (a second
// finger, a second mouse button) are ignored until the active gesture ends,
// so a stray touch can't overwrite the gesture's start position.

import type { Attachment } from 'svelte/attachments';

export type SwipeHandlers = {
	onLeft?: () => void;
	onRight?: () => void;
};

const SWIPE_THRESHOLD_PX = 50;

export function swipe(handlers: SwipeHandlers): Attachment<HTMLElement> {
	return (node: HTMLElement) => {
		let startX = 0;
		let startY = 0;
		let activePointerId: number | null = null;

		const onPointerDown = (event: PointerEvent) => {
			if (activePointerId !== null) return;
			activePointerId = event.pointerId;
			startX = event.clientX;
			startY = event.clientY;
		};

		const onPointerUp = (event: PointerEvent) => {
			if (event.pointerId !== activePointerId) return;
			activePointerId = null;
			const dx = event.clientX - startX;
			const dy = event.clientY - startY;
			// Dominant vertical motion is a scroll, not a swipe.
			if (Math.abs(dy) > Math.abs(dx)) return;
			if (dx <= -SWIPE_THRESHOLD_PX) {
				handlers.onLeft?.();
			} else if (dx >= SWIPE_THRESHOLD_PX) {
				handlers.onRight?.();
			}
		};

		const onPointerCancel = (event: PointerEvent) => {
			if (event.pointerId !== activePointerId) return;
			activePointerId = null;
		};

		node.addEventListener('pointerdown', onPointerDown);
		node.addEventListener('pointerup', onPointerUp);
		node.addEventListener('pointercancel', onPointerCancel);

		return () => {
			node.removeEventListener('pointerdown', onPointerDown);
			node.removeEventListener('pointerup', onPointerUp);
			node.removeEventListener('pointercancel', onPointerCancel);
		};
	};
}
