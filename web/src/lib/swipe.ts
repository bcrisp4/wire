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
// pointerup is bound on the element itself rather than `window` so a swipe
// that ends outside the element (e.g. the pointer drifts off the edge) is
// dropped on the floor rather than firing for the wrong card.

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
		let tracking = false;

		const onPointerDown = (event: PointerEvent) => {
			startX = event.clientX;
			startY = event.clientY;
			tracking = true;
		};

		const onPointerUp = (event: PointerEvent) => {
			if (!tracking) return;
			tracking = false;
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

		const onPointerCancel = () => {
			tracking = false;
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
