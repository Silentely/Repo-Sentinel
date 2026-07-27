import { z } from "zod";

const requiredUsername = z.string().trim().min(1, "请输入用户名。").max(128, "用户名过长。");
const requiredPassword = z.string().min(1, "请输入密码。");
const setupPassword = requiredPassword.refine((value) => Array.from(value).length >= 12, {
  message: "密码至少需要 12 个 Unicode 字符。",
});

export const loginSchema = z.object({
  username: requiredUsername,
  password: requiredPassword,
});

export const setupSchema = z
  .object({
    username: requiredUsername,
    password: setupPassword,
    confirmPassword: requiredPassword,
  })
  .superRefine((value, context) => {
    if (value.password !== value.confirmPassword) {
      context.addIssue({
        code: "custom",
        path: ["confirmPassword"],
        message: "两次输入的密码不一致。",
      });
    }
  });

export type LoginCredentials = z.input<typeof loginSchema>;
export type SetupFormValues = z.input<typeof setupSchema>;
