"use client";

import { useCallback, useState } from "react";

import { NetworkError } from "../../lib/api";

type PendingMutation<T> = { input: T; key: string };

function newIdempotencyKey(): string {
  return crypto.randomUUID();
}

export function useIdempotentMutation<T, Result>(mutate: (input: T, key: string) => Promise<Result>) {
  const [pending, setPending] = useState<PendingMutation<T> | null>(null);
  const [isSubmitting, setIsSubmitting] = useState(false);

  const attempt = useCallback(async (operation: PendingMutation<T>) => {
    setIsSubmitting(true);
    try {
      const result = await mutate(operation.input, operation.key);
      setPending(null);
      return result;
    } catch (error) {
      if (error instanceof NetworkError) setPending(operation);
      else setPending(null);
      throw error;
    } finally {
      setIsSubmitting(false);
    }
  }, [mutate]);

  const submit = useCallback((input: T) => attempt({ input, key: newIdempotencyKey() }), [attempt]);
  const retry = useCallback(() => pending ? attempt(pending) : Promise.resolve(undefined), [attempt, pending]);

  return { submit, retry, hasRetry: pending !== null, isSubmitting };
}
