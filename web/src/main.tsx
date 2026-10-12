import React from "react";
import ReactDOM from "react-dom/client";
import { MutationCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter } from "react-router";
import "@fontsource-variable/inter/wght.css";
import "./styles.css";
import { App } from "./app";
import { Toaster, friendlyError, toast } from "@/components/ui";

declare module "@tanstack/react-query" {
  interface Register {
    mutationMeta: {
      /** What the action was, for the failure toast: "Couldn't {action}". */
      action?: string;
      /** The page shows this mutation's error itself; don't toast it. */
      inlineError?: boolean;
    };
  }
}

const queryClient = new QueryClient({
  defaultOptions: {
    queries: { staleTime: 10_000, refetchOnWindowFocus: false, retry: 1 },
  },
  // No action fails silently: every failed change shows a toast unless the page shows it inline.
  mutationCache: new MutationCache({
    onError: (error, _vars, _ctx, mutation) => {
      if (mutation.meta?.inlineError) return;
      const f = friendlyError(error);
      toast.error(mutation.meta?.action ? `Couldn't ${mutation.meta.action}` : "That change didn't go through", f.detail || f.title);
    },
  }),
});

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter basename="/ui">
        <App />
        <Toaster />
      </BrowserRouter>
    </QueryClientProvider>
  </React.StrictMode>,
);
