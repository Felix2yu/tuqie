import { useEffect, useRef } from 'react';

/**
 * 留一份「最新提交值」的引用，供异步回调（事件处理器 / 定时器 / SSE / SW 消息）读取。
 *
 * 为什么在 effect 里更新，而不是渲染期直接写 `ref.current = value`：
 * 渲染期写 ref 在并发渲染（StrictMode 双渲染、时间切片、可中断渲染）下会读到
 * 半成品状态——同一次渲染被丢弃重来时，ref 里留下的是废弃渲染写进去的值。
 * effect 在提交之后运行，异步回调读到的始终是已提交渲染的值，语义与原写法一致。
 */
export function useLatest<T>(value: T) {
  const ref = useRef(value);
  useEffect(() => {
    ref.current = value;
  });
  return ref;
}
