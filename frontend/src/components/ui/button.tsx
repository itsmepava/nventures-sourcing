// shadcn/ui Button pattern: local source, Radix Slot and class variance authority.
import * as React from 'react'
import { Slot } from '@radix-ui/react-slot'
import { cva, type VariantProps } from 'class-variance-authority'
import { cn } from '../../utils'
const variants = cva('inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-lg text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-sky-600 disabled:pointer-events-none disabled:opacity-50 cursor-pointer', { variants: { variant: { default: 'bg-[#1391d4] text-white hover:bg-[#0879b6]', outline: 'border border-[#dce3eb] bg-white text-[#243449] hover:bg-[#f1f6fa]', ghost: 'text-[#57697c] hover:bg-[#edf5fb]' }, size: { default: 'h-10 px-4 py-2', sm: 'h-8 px-3 text-xs', icon: 'h-10 w-10' } }, defaultVariants: { variant: 'default', size: 'default' } })
type Props = React.ButtonHTMLAttributes<HTMLButtonElement> & VariantProps<typeof variants> & { asChild?: boolean }
export const Button = React.forwardRef<HTMLButtonElement, Props>(({ className, variant, size, asChild = false, ...props }, ref) => { const Comp = asChild ? Slot : 'button'; return <Comp className={cn(variants({ variant, size }), className)} ref={ref} {...props} /> })
Button.displayName = 'Button'
