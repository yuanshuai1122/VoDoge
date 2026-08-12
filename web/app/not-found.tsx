import Link from "next/link";
import { buttonVariants } from "@/components/ui/button";

export default function NotFound() {
  return (
    <div className="flex min-h-svh flex-col items-center justify-center gap-4 text-center">
      <div>
        <h2 className="text-lg font-semibold">页面不存在</h2>
        <p className="mt-1 text-sm text-muted-foreground">
          请求的地址没有对应的页面。
        </p>
      </div>

      <Link href="/" className={buttonVariants({ size: "sm" })}>
        返回仪表盘
      </Link>
    </div>
  );
}
