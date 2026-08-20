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

// Radix's Select (the Status filter) drives its trigger with pointer capture,
// which jsdom also leaves unimplemented.
if (!Element.prototype.hasPointerCapture) {
  Element.prototype.hasPointerCapture = () => false;
  Element.prototype.setPointerCapture = () => {};
  Element.prototype.releasePointerCapture = () => {};
}

// components/ui/sidebar.tsx decides between the desktop sidebar and the mobile
// drawer from a media query jsdom doesn't implement. Reporting "no match"
// keeps every test on the desktop branch, which is the one the screens are
// written against.
if (!window.matchMedia) {
  window.matchMedia = (query: string) =>
    ({
      matches: false,
      media: query,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    }) as MediaQueryList;
}
