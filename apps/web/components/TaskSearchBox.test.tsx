import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, fireEvent, act } from "@testing-library/react";
import { TaskSearchBox } from "./TaskSearchBox";

describe("TaskSearchBox", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("shows the initial value", () => {
    render(<TaskSearchBox value="urgent" onChange={vi.fn()} />);
    expect(screen.getByRole("textbox", { name: "Search tasks" })).toHaveValue("urgent");
  });

  it("updates the input immediately but only calls onChange after the debounce elapses", () => {
    vi.useFakeTimers();
    const onChange = vi.fn();
    render(<TaskSearchBox value="" onChange={onChange} />);

    const input = screen.getByRole("textbox", { name: "Search tasks" });
    fireEvent.change(input, { target: { value: "login" } });

    expect(input).toHaveValue("login");
    expect(onChange).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(299);
    });
    expect(onChange).not.toHaveBeenCalled();

    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(onChange).toHaveBeenCalledWith("login");
    expect(onChange).toHaveBeenCalledTimes(1);
  });

  it("only fires once for rapid keystrokes, restarting the debounce each time", () => {
    vi.useFakeTimers();
    const onChange = vi.fn();
    render(<TaskSearchBox value="" onChange={onChange} />);

    const input = screen.getByRole("textbox", { name: "Search tasks" });
    fireEvent.change(input, { target: { value: "l" } });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    fireEvent.change(input, { target: { value: "lo" } });
    act(() => {
      vi.advanceTimersByTime(200);
    });
    fireEvent.change(input, { target: { value: "log" } });
    act(() => {
      vi.advanceTimersByTime(300);
    });

    expect(onChange).toHaveBeenCalledTimes(1);
    expect(onChange).toHaveBeenCalledWith("log");
  });

  it("syncs to a value change from outside, such as the back button", () => {
    const { rerender } = render(<TaskSearchBox value="" onChange={vi.fn()} />);
    rerender(<TaskSearchBox value="restored" onChange={vi.fn()} />);
    expect(screen.getByRole("textbox", { name: "Search tasks" })).toHaveValue("restored");
  });
});
