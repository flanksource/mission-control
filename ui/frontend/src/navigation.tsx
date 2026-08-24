import { Link, useLocation, useNavigate, type LinkProps } from "react-router-dom";
import { useMemo, type AnchorHTMLAttributes, type ReactNode } from "react";
import {
  RouterProvider,
  type RenderLink,
  type RouterAdapter,
} from "@flanksource/clicky-ui/rpc";

export const UI_BASE = "/ui";

export function routerPathFromHref(href: string): string {
  if (href === UI_BASE) return "/";
  if (href.startsWith(`${UI_BASE}/`)) return href.slice(UI_BASE.length);
  return href;
}

export function uiHref(path: string): string {
  return `${UI_BASE}${path.startsWith("/") ? path : `/${path}`}`;
}

export function isUiHref(href: string): boolean {
  return href === UI_BASE || href.startsWith(`${UI_BASE}/`);
}

type AppLinkProps = Omit<AnchorHTMLAttributes<HTMLAnchorElement>, "href"> & {
  href: string;
  children?: ReactNode;
};

export function AppLink({ href, children, ...props }: AppLinkProps) {
  if (!isUiHref(href)) {
    return <a href={href} {...props}>{children}</a>;
  }

  return (
    <Link to={routerPathFromHref(href)} {...(props as Omit<LinkProps, "to">)}>
      {children}
    </Link>
  );
}

export const renderLink: RenderLink = ({ to, className, children, title, key }) => (
  <Link key={key} to={routerPathFromHref(to)} className={className} title={title}>
    {children}
  </Link>
);

// clicky-ui nav surfaces read routing through RouterContext; this bridges it to
// react-router, translating between /ui-prefixed hrefs and router paths.
export function AppRouterProvider({ children }: { children: ReactNode }) {
  const location = useLocation();
  const navigate = useNavigate();

  const adapter = useMemo<RouterAdapter>(
    () => ({
      pathname: `${UI_BASE}${location.pathname}`,
      renderLink,
      navigate: (to, opts) =>
        navigate(routerPathFromHref(to), { replace: opts?.replace }),
    }),
    [location.pathname, navigate],
  );

  return <RouterProvider adapter={adapter}>{children}</RouterProvider>;
}
