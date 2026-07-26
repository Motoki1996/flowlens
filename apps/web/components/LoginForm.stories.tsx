import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import { http, HttpResponse, delay } from "msw";
import { LoginForm } from "./LoginForm";
import { API_PUBLIC_URL } from "@/lib/config";

const meta = {
  title: "Components/LoginForm",
  component: LoginForm,
} satisfies Meta<typeof LoginForm>;

export default meta;
type Story = StoryObj<typeof meta>;

async function fillAndSubmit(canvas: ReturnType<typeof within>) {
  await userEvent.type(canvas.getByLabelText("Username or email"), "octocat");
  await userEvent.type(canvas.getByLabelText("Password"), "correct-horse-battery-staple");
  await userEvent.click(canvas.getByRole("button", { name: /sign in/i }));
}

export const Default: Story = {};

export const Error: Story = {
  parameters: {
    msw: {
      handlers: [
        http.post(`${API_PUBLIC_URL}/auth/login`, () =>
          HttpResponse.json({ error: { message: "Invalid username or password." } }, { status: 401 }),
        ),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await fillAndSubmit(canvas);
    await expect(await canvas.findByText("Invalid username or password.")).toBeInTheDocument();
  },
};

export const Submitting: Story = {
  parameters: {
    msw: {
      handlers: [
        http.post(`${API_PUBLIC_URL}/auth/login`, async () => {
          await delay("infinite");
        }),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await fillAndSubmit(canvas);
    await expect(await canvas.findByRole("button", { name: /signing in/i })).toBeDisabled();
  },
};
