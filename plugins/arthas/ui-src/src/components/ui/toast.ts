export const toastManager = {
  add({ title, type }: { title: string; type?: "success" | "error" | "info" }) {
    if (type === "error") console.error(title);
    else console.info(title);
  },
};
