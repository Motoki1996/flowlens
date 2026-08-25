import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import type { Velocity, VelocityPeriod } from "@/types";
import { VelocitySection } from "./VelocitySection";

function makePeriod(overrides: Partial<VelocityPeriod>): VelocityPeriod {
  return {
    start: "2026-08-10T00:00:00Z",
    end: "2026-08-17T00:00:00Z",
    completed: 0,
    completedByUser: 0,
    completedByAgent: 0,
    completedByUnknown: 0,
    completedPoints: 0,
    completedPointsByUser: 0,
    completedPointsByAgent: 0,
    completedPointsByUnknown: 0,
    movingAverage: 0,
    movingAveragePoints: 0,
    complete: true,
    ...overrides,
  };
}

function makeVelocity(overrides: Partial<Velocity>): Velocity {
  return {
    from: null,
    to: null,
    interval: "week",
    truncated: false,
    periods: [],
    openTaskCount: 0,
    averageVelocity: null,
    forecastPeriods: null,
    openTaskPoints: 0,
    averageVelocityPoints: null,
    forecastPeriodsByPoints: null,
    sizedTaskRatio: 0,
    unbrokenDownEpicPoints: 0,
    unestimatedEpicCount: 0,
    openPointsTotal: 0,
    ...overrides,
  };
}

/** weekOf builds one consecutive week's period from its actor splits in both
 *  units, so the stories below stay readable instead of drowning in literal
 *  objects. Week 0 starts 2026-06-22 (a Monday, matching the API's ISO-week
 *  bucket starts). */
function weekOf(
  index: number,
  tasks: { user: number; agent: number; unknown: number },
  points: { user: number; agent: number; unknown: number },
  movingAverage: number,
  movingAveragePoints: number,
): VelocityPeriod {
  const start = new Date(Date.UTC(2026, 5, 22 + index * 7));
  const end = new Date(Date.UTC(2026, 5, 29 + index * 7));
  return {
    start: start.toISOString(),
    end: end.toISOString(),
    completed: tasks.user + tasks.agent + tasks.unknown,
    completedByUser: tasks.user,
    completedByAgent: tasks.agent,
    completedByUnknown: tasks.unknown,
    completedPoints: points.user + points.agent + points.unknown,
    completedPointsByUser: points.user,
    completedPointsByAgent: points.agent,
    completedPointsByUnknown: points.unknown,
    movingAverage,
    movingAveragePoints,
    complete: true,
  };
}

const meta = {
  title: "Components/VelocitySection",
  component: VelocitySection,
} satisfies Meta<typeof VelocitySection>;

export default meta;
type Story = StoryObj<typeof meta>;

/** NoData: no completed tasks yet, or none in the selected range. */
export const NoData: Story = {
  args: { velocity: makeVelocity({}) },
};

/**
 * Normal: eight weeks of a healthy, gently climbing stream, split across all
 * three actors, with the most recent (partial) week visibly dimmer than the
 * rest — the shape this chart is meant to make legible at a glance. Sizes
 * are being set here (sizedTaskRatio 0.8), so the Points tab tells a
 * genuinely different story from Tasks: week 5 finished fewer tasks than
 * week 4 but more points, because the work was bigger.
 */
export const Normal: Story = {
  args: {
    velocity: makeVelocity({
      openTaskCount: 34,
      averageVelocity: 9.5,
      forecastPeriods: 3.6,
      openTaskPoints: 121,
      averageVelocityPoints: 31.5,
      forecastPeriodsByPoints: 3.8,
      sizedTaskRatio: 0.8,
      periods: [
        weekOf(0, { user: 3, agent: 2, unknown: 1 }, { user: 9, agent: 8, unknown: 3 }, 6, 20),
        weekOf(1, { user: 3, agent: 3, unknown: 1 }, { user: 9, agent: 11, unknown: 3 }, 6.5, 21.5),
        weekOf(2, { user: 4, agent: 3, unknown: 1 }, { user: 13, agent: 12, unknown: 3 }, 7, 24),
        weekOf(3, { user: 4, agent: 4, unknown: 1 }, { user: 12, agent: 18, unknown: 2 }, 7.5, 26.5),
        // Fewer tasks than the week before, but more points: bigger work.
        weekOf(4, { user: 3, agent: 3, unknown: 1 }, { user: 16, agent: 21, unknown: 5 }, 8.5, 31),
        weekOf(5, { user: 5, agent: 5, unknown: 1 }, { user: 15, agent: 19, unknown: 3 }, 9.5, 32),
        weekOf(6, { user: 5, agent: 6, unknown: 1 }, { user: 16, agent: 22, unknown: 4 }, 10.5, 34),
        // The current, still-running week — always drawn dimmer, so its
        // necessarily-low count never reads as a slowdown.
        { ...weekOf(7, { user: 2, agent: 2, unknown: 0 }, { user: 6, agent: 7, unknown: 0 }, 9.25, 29.5), complete: false },
      ],
    }),
  },
};

/** NoForecastYet: some tasks have completed, but none in a *complete*
 *  period — averageVelocity and forecastPeriods stay null and the stat row
 *  shows a placeholder rather than a misleading number. */
export const NoForecastYet: Story = {
  args: {
    velocity: makeVelocity({
      openTaskCount: 5,
      averageVelocity: null,
      forecastPeriods: null,
      periods: [
        makePeriod({ completed: 0 }),
        makePeriod({
          start: "2026-08-17T00:00:00Z",
          end: "2026-08-24T00:00:00Z",
          completed: 2,
          completedByUser: 1,
          completedByUnknown: 1,
          movingAverage: 1,
          complete: false,
        }),
      ],
    }),
  },
};

/** Truncated: the covered range would exceed 52 buckets, so only the newest
 *  52 weeks are returned and a one-line note says so. */
export const Truncated: Story = {
  args: {
    velocity: makeVelocity({
      truncated: true,
      openTaskCount: 12,
      averageVelocity: 4.25,
      forecastPeriods: 2.8,
      periods: Array.from({ length: 6 }, (_, i) => {
        const start = new Date(Date.UTC(2026, 6, 6 + i * 7));
        const end = new Date(Date.UTC(2026, 6, 13 + i * 7));
        return makePeriod({
          start: start.toISOString(),
          end: end.toISOString(),
          completed: 4 + i,
          completedByUser: 2,
          completedByAgent: 1 + i,
          completedByUnknown: 1,
          movingAverage: 4 + i / 2,
          complete: i < 5,
        });
      }),
    }),
  },
};

/** NothingSized: real throughput, but nobody has set a size on any completed
 *  task, so every one of them is still the default M. The Points tab is
 *  arithmetically 3x the Tasks tab and says so rather than implying a second
 *  measurement — the state every project is in immediately after the size
 *  column ships. */
export const NothingSized: Story = {
  args: {
    velocity: makeVelocity({
      openTaskCount: 12,
      averageVelocity: 5,
      forecastPeriods: 2.4,
      openTaskPoints: 36,
      averageVelocityPoints: 15,
      forecastPeriodsByPoints: 2.4,
      sizedTaskRatio: 0,
      periods: [
        weekOf(0, { user: 2, agent: 2, unknown: 0 }, { user: 6, agent: 6, unknown: 0 }, 4, 12),
        weekOf(1, { user: 3, agent: 2, unknown: 1 }, { user: 9, agent: 6, unknown: 3 }, 5, 15),
      ],
    }),
  },
};

/** Error: the velocity request failed; the rest of the Project view still
 *  renders around it. */
export const Error: Story = {
  args: { velocity: null, error: true },
};
