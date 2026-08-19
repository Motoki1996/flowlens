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
    movingAverage: 0,
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
    ...overrides,
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
 * rest — the shape this chart is meant to make legible at a glance.
 */
export const Normal: Story = {
  args: {
    velocity: makeVelocity({
      openTaskCount: 34,
      averageVelocity: 9.5,
      forecastPeriods: 3.6,
      periods: [
        {
          start: "2026-06-22T00:00:00Z",
          end: "2026-06-29T00:00:00Z",
          completed: 6,
          completedByUser: 3,
          completedByAgent: 2,
          completedByUnknown: 1,
          movingAverage: 6,
          complete: true,
        },
        {
          start: "2026-06-29T00:00:00Z",
          end: "2026-07-06T00:00:00Z",
          completed: 7,
          completedByUser: 3,
          completedByAgent: 3,
          completedByUnknown: 1,
          movingAverage: 6.5,
          complete: true,
        },
        {
          start: "2026-07-06T00:00:00Z",
          end: "2026-07-13T00:00:00Z",
          completed: 8,
          completedByUser: 4,
          completedByAgent: 3,
          completedByUnknown: 1,
          movingAverage: 7,
          complete: true,
        },
        {
          start: "2026-07-13T00:00:00Z",
          end: "2026-07-20T00:00:00Z",
          completed: 9,
          completedByUser: 4,
          completedByAgent: 4,
          completedByUnknown: 1,
          movingAverage: 7.5,
          complete: true,
        },
        {
          start: "2026-07-20T00:00:00Z",
          end: "2026-07-27T00:00:00Z",
          completed: 10,
          completedByUser: 4,
          completedByAgent: 5,
          completedByUnknown: 1,
          movingAverage: 8.5,
          complete: true,
        },
        {
          start: "2026-07-27T00:00:00Z",
          end: "2026-08-03T00:00:00Z",
          completed: 11,
          completedByUser: 5,
          completedByAgent: 5,
          completedByUnknown: 1,
          movingAverage: 9.5,
          complete: true,
        },
        {
          start: "2026-08-03T00:00:00Z",
          end: "2026-08-10T00:00:00Z",
          completed: 12,
          completedByUser: 5,
          completedByAgent: 6,
          completedByUnknown: 1,
          movingAverage: 10.5,
          complete: true,
        },
        {
          // The current, still-running week — always drawn dimmer, so its
          // necessarily-low count never reads as a slowdown.
          start: "2026-08-10T00:00:00Z",
          end: "2026-08-17T00:00:00Z",
          completed: 4,
          completedByUser: 2,
          completedByAgent: 2,
          completedByUnknown: 0,
          movingAverage: 9.25,
          complete: false,
        },
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

/** Error: the velocity request failed; the rest of the Project view still
 *  renders around it. */
export const Error: Story = {
  args: { velocity: null, error: true },
};
