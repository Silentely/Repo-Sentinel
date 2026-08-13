import { useEffect, useState } from "react";

interface NumberFieldProps {
  /** 受控数值（外部表单状态）。 */
  value: number;
  /** 允许输入范围，失焦时钳制到 [min, max]。 */
  min?: number;
  max?: number;
  /** 整数输入：失焦时截断小数部分（如重试次数）。 */
  integer?: boolean;
  disabled?: boolean;
  placeholder?: string;
  onChange: (value: number) => void;
}

/**
 * 数字输入框：输入过程不做即时钳制，允许逐位自由输入；
 * 失焦时钳到 [min, max]（可选截断为整数）并上抛最终值。
 * 解决即时钳制导致多位数无法逐位输入的问题（如 max_tokens 键入首位数即被钳到 100）。
 */
export function NumberField({ value, min, max, integer, disabled, placeholder, onChange }: NumberFieldProps) {
  const [text, setText] = useState(String(value));
  const [focused, setFocused] = useState(false);

  // 数字等宽：宽度按上限位数（ch）+ 左右 padding/border 计算，同一字段内固定、不随输入伸缩，
  // 避免 fit-content/field-sizing 跨浏览器行为不一致（field-sizing 仅 Chrome/Safari 支持）。
  // 下限 4ch 保证聚焦时数字 spinner 有空间，上限 16ch 避免超大上限撑爆布局。
  const widthChars = Math.min(Math.max(String(max ?? 9999).length + 2, 4), 16);
  const inputWidth = `calc(${widthChars}ch + 1.5rem + 2px)`;

  // 外部值变化（表单回填/重置）时同步显示；聚焦编辑中不覆盖用户输入。
  useEffect(() => {
    if (!focused) setText(String(value));
  }, [value, focused]);

  const commit = (raw: string) => {
    const trimmed = raw.trim();
    if (trimmed === "") {
      const fallback = min ?? 0;
      setText(String(fallback));
      onChange(fallback);
      return;
    }
    let n = Number(trimmed);
    if (!Number.isFinite(n)) {
      setText(String(value));
      return;
    }
    if (integer) n = Math.trunc(n);
    if (min !== undefined) n = Math.max(min, n);
    if (max !== undefined) n = Math.min(max, n);
    setText(String(n));
    onChange(n);
  };

  return (
    <input
      type="number"
      inputMode={integer ? "numeric" : "decimal"}
      min={min}
      max={max}
      step={integer ? 1 : undefined}
      value={text}
      disabled={disabled}
      placeholder={placeholder}
      style={{ width: inputWidth }}
      onFocus={() => setFocused(true)}
      onBlur={() => {
        setFocused(false);
        commit(text);
      }}
      onChange={(e) => {
        const raw = e.target.value;
        setText(raw);
        const trimmed = raw.trim();
        if (trimmed !== "" && Number.isFinite(Number(trimmed))) {
          // 输入合法即上抛原始值供提交使用；范围钳制交给失焦。
          onChange(Number(trimmed));
        }
      }}
    />
  );
}
