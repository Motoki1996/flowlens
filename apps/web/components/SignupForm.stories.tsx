import type { Meta, StoryObj } from "@storybook/nextjs-vite";
import { expect, userEvent, within } from "storybook/test";
import { http, HttpResponse, delay } from "msw";
import { SignupForm } from "./SignupForm";
import { API_PUBLIC_URL } from "@/lib/config";

const meta = {
  title: "Components/SignupForm",
  component: SignupForm,
} satisfies Meta<typeof SignupForm>;

export default meta;
type Story = StoryObj<typeof meta>;

async function fillAndSubmit(canvas: ReturnType<typeof within>) {
  await userEvent.type(canvas.getByLabelText("Username"), "octocat");
  await userEvent.type(canvas.getByLabelText("Email"), "octocat@example.com");
  await userEvent.type(canvas.getByLabelText("Password"), "correct-horse-battery-staple");
  await userEvent.type(canvas.getByLabelText("Confirm password"), "correct-horse-battery-staple");
  await userEvent.click(canvas.getByRole("button", { name: /create account/i }));
}

export const Default: Story = {};

export const Error: Story = {
  parameters: {
    msw: {
      handlers: [
        http.post(`${API_PUBLIC_URL}/auth/signup`, () =>
          HttpResponse.json({ error: { message: "Username is already taken." } }, { status: 409 }),
        ),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await fillAndSubmit(canvas);
    await expect(await canvas.findByText("Username is already taken.")).toBeInTheDocument();
  },
};

export const Submitting: Story = {
  parameters: {
    msw: {
      handlers: [
        http.post(`${API_PUBLIC_URL}/auth/signup`, async () => {
          await delay("infinite");
        }),
      ],
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await fillAndSubmit(canvas);
    await expect(await canvas.findByRole("button", { name: /creating account/i })).toBeDisabled();
  },
};

export const PasswordMismatch: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.type(canvas.getByLabelText("Username"), "octocat");
    await userEvent.type(canvas.getByLabelText("Email"), "octocat@example.com");
    await userEvent.type(canvas.getByLabelText("Password"), "correct-horse-battery-staple");
    await userEvent.type(canvas.getByLabelText("Confirm password"), "correct-horse-battery-stapl");
    await userEvent.click(canvas.getByRole("button", { name: /create account/i }));
    await expect(await canvas.findByText("Password and confirmation do not match.")).toBeInTheDocument();
  },
};

export const PasswordRevealed: Story = {
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.type(canvas.getByLabelText("Password"), "correct-horse-battery-staple");
    await userEvent.click(canvas.getByRole("button", { name: "Show password" }));
    await expect(canvas.getByLabelText("Password")).toHaveAttribute("type", "text");
  },
};
