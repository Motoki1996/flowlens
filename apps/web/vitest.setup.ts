import "@testing-library/jest-dom/vitest";

// jsdom implements neither of these, and cmdk (the search-and-select list
// behind our Combobox) calls both on mount — a ResizeObserver to size the list
// and scrollIntoView to keep the highlighted item visible.
if (!globalThis.ResizeObserver) {
  globalThis.ResizeObserver = class ResizeObserver {
    constructor(private callback: ResizeObserverCallback) {}

    // jsdom lays nothing out, so an observed element would report 0×0 and
    // recharts' ResponsiveContainer would collapse. Report a plausible box
    // instead, which keeps charts measurable while cmdk gets its observer.
    observe(target: Element) {
      this.callback(
        [{ target, contentRect: { width: 800, height: 400 } } as ResizeObserverEntry],
        this as unknown as ResizeObserver,
      );
    }

    unobserve() {}
    disconnect() {}
  };
}

if (!Element.prototype.scrollIntoView) {
  Element.prototype.scrollIntoView = function scrollIntoView() {};
}
