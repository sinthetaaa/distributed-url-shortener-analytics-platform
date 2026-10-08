"use client";

import { FormEvent, useState } from "react";

import styles from "./auth.module.css";

type AuthMode = "login" | "register";

type AuthFormProps = {
  mode: AuthMode;
};

type FieldErrors = {
  email?: string;
  password?: string;
  confirmPassword?: string;
};

const MAX_PASSWORD_BYTES = 72;

function passwordBytes(value: string) {
  return new TextEncoder().encode(value).length;
}

function isValidEmail(value: string) {
  return /^[^\\s@]+@[^\\s@]+\\.[^\\s@]+$/.test(value);
}

export function AuthForm({ mode }: AuthFormProps) {
  const isRegister = mode === "register";

  const [showPassword, setShowPassword] = useState(false);
  const [showConfirmPassword, setShowConfirmPassword] = useState(false);
  const [errors, setErrors] = useState<FieldErrors>({});

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();

    const form = new FormData(event.currentTarget);

    const email = String(form.get("email") ?? "").trim();
    const password = String(form.get("password") ?? "");
    const confirmPassword = String(form.get("confirmPassword") ?? "");

    const nextErrors: FieldErrors = {};

    if (!email) {
      nextErrors.email = "Enter your email.";
    } else if (!isValidEmail(email)) {
      nextErrors.email = "Enter a valid email address.";
    }

    if (!password) {
      nextErrors.password = "Enter your password.";
    }

    if (isRegister && password) {
      if (password.length < 8) {
        nextErrors.password = "Use at least 8 characters.";
      } else if (passwordBytes(password) > MAX_PASSWORD_BYTES) {
        nextErrors.password = "Password must be 72 bytes or fewer.";
      }

      if (!confirmPassword) {
        nextErrors.confirmPassword = "Confirm your password.";
      } else if (password !== confirmPassword) {
        nextErrors.confirmPassword = "Passwords don’t match.";
      }
    }

    setErrors(nextErrors);
  }

  return (
    <form className={styles.form} onSubmit={handleSubmit} noValidate>
      <div className={styles.field}>
        <label className={styles.label} htmlFor={`${mode}-email`}>
          Email
        </label>

        <input
          className={styles.input}
          id={`${mode}-email`}
          name="email"
          type="email"
          inputMode="email"
          autoComplete="email"
          aria-invalid={Boolean(errors.email)}
          aria-describedby={errors.email ? `${mode}-email-error` : undefined}
          required
        />

        {errors.email ? (
          <p
            className={styles.error}
            id={`${mode}-email-error`}
            role="alert"
          >
            {errors.email}
          </p>
        ) : null}
      </div>

      <div className={styles.field}>
        <label className={styles.label} htmlFor={`${mode}-password`}>
          Password
        </label>

        <div className={styles.passwordField}>
          <input
            className={`${styles.input} ${styles.passwordInput}`}
            id={`${mode}-password`}
            name="password"
            type={showPassword ? "text" : "password"}
            autoComplete={isRegister ? "new-password" : "current-password"}
            aria-invalid={Boolean(errors.password)}
            aria-describedby={
              errors.password
                ? `${mode}-password-error`
                : isRegister
                  ? `${mode}-password-help`
                  : undefined
            }
            required
          />

          <button
            className={styles.passwordToggle}
            type="button"
            onClick={() => setShowPassword((value) => !value)}
            aria-label={showPassword ? "Hide password" : "Show password"}
          >
            {showPassword ? "Hide" : "Show"}
          </button>
        </div>

        {isRegister && !errors.password ? (
          <p className={styles.help} id={`${mode}-password-help`}>
            At least 8 characters.
          </p>
        ) : null}

        {errors.password ? (
          <p
            className={styles.error}
            id={`${mode}-password-error`}
            role="alert"
          >
            {errors.password}
          </p>
        ) : null}
      </div>

      {isRegister ? (
        <div className={styles.field}>
          <label
            className={styles.label}
            htmlFor={`${mode}-confirm-password`}
          >
            Confirm password
          </label>

          <div className={styles.passwordField}>
            <input
              className={`${styles.input} ${styles.passwordInput}`}
              id={`${mode}-confirm-password`}
              name="confirmPassword"
              type={showConfirmPassword ? "text" : "password"}
              autoComplete="new-password"
              aria-invalid={Boolean(errors.confirmPassword)}
              aria-describedby={
                errors.confirmPassword
                  ? `${mode}-confirm-password-error`
                  : undefined
              }
              required
            />

            <button
              className={styles.passwordToggle}
              type="button"
              onClick={() =>
                setShowConfirmPassword((value) => !value)
              }
              aria-label={
                showConfirmPassword
                  ? "Hide confirmed password"
                  : "Show confirmed password"
              }
            >
              {showConfirmPassword ? "Hide" : "Show"}
            </button>
          </div>

          {errors.confirmPassword ? (
            <p
              className={styles.error}
              id={`${mode}-confirm-password-error`}
              role="alert"
            >
              {errors.confirmPassword}
            </p>
          ) : null}
        </div>
      ) : null}

      <button className={styles.primaryButton} type="submit">
        {isRegister ? "Create account" : "Sign in"}
      </button>
    </form>
  );
}
