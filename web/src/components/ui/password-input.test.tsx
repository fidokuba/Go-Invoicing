import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { PasswordInput } from "./password-input";
import { Label } from "./input";

afterEach(cleanup);

function renderField(onSubmit = vi.fn()) {
  render(
    <form
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit();
      }}
    >
      <Label htmlFor="pw">Password</Label>
      <PasswordInput id="pw" defaultValue="s3cret-value" />
    </form>,
  );
  return { input: screen.getByLabelText("Password", { exact: true }), onSubmit };
}

describe("PasswordInput", () => {
  it("starts hidden, and the toggle reveals and hides it again", async () => {
    const user = userEvent.setup();
    const { input } = renderField();

    expect(input).toHaveAttribute("type", "password");

    await user.click(screen.getByRole("button", { name: "Show password" }));
    expect(input).toHaveAttribute("type", "text");
    expect(input).toHaveValue("s3cret-value");

    await user.click(screen.getByRole("button", { name: "Hide password" }));
    expect(input).toHaveAttribute("type", "password");
  });

  it("never submits the form", async () => {
    const user = userEvent.setup();
    const { onSubmit } = renderField();

    await user.click(screen.getByRole("button", { name: "Show password" }));

    expect(onSubmit).not.toHaveBeenCalled();
  });
});
