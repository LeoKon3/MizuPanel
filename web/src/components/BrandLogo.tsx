type BrandLogoProps = {
  className?: string
}

export function BrandLogo({ className = 'h-9 w-9' }: BrandLogoProps) {
  return (
    <img
      src="/logo.svg"
      alt="MizuPanel logo"
      aria-label="MizuPanel logo"
      data-testid="mizupanel-logo-mark"
      width={32}
      height={32}
      draggable={false}
      className={`block shrink-0 ${className}`}
    />
  )
}
