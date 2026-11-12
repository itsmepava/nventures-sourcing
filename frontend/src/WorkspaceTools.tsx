import { useEffect, useRef, useState } from 'react'
import { Moon, Sun, Monitor, Search, Keyboard, X } from 'lucide-react'
import { usePreference } from './preferences'
const pages=['Overview','Companies','Pitch inbox','Sourcing runs','Partners','Audit trail','Settings']
export default function WorkspaceTools({navigate,refresh}:{navigate:(page:string)=>void;refresh:()=>void}) {
 const [theme,setTheme]=usePreference('theme','system'),[compact,setCompact]=usePreference('compact',false)
 const [open,setOpen]=useState(false),[query,setQuery]=useState(''),[offline,setOffline]=useState(!navigator.onLine)
 const input=useRef<HTMLInputElement>(null)
 const returnFocus=useRef<HTMLElement|null>(null)
 function show(){returnFocus.current=document.activeElement as HTMLElement;setOpen(true)}
 useEffect(()=>{const media=matchMedia('(prefers-color-scheme: dark)');const apply=()=>{document.documentElement.dataset.theme=theme==='system'?(media.matches?'dark':'light'):theme;document.documentElement.style.colorScheme=document.documentElement.dataset.theme};apply();media.addEventListener('change',apply);return()=>media.removeEventListener('change',apply)},[theme])
 useEffect(()=>{document.documentElement.dataset.density=compact?'compact':'comfortable'},[compact])
 useEffect(()=>{const online=()=>setOffline(!navigator.onLine);window.addEventListener('online',online);window.addEventListener('offline',online);return()=>{window.removeEventListener('online',online);window.removeEventListener('offline',online)}},[])
 useEffect(()=>{function key(e:KeyboardEvent){const editing=(e.target as HTMLElement)?.matches('input,textarea,select,[contenteditable]');if((e.ctrlKey||e.metaKey)&&e.key.toLowerCase()==='k'){e.preventDefault();if(!open)returnFocus.current=document.activeElement as HTMLElement;setOpen(x=>!x)}else if(!editing&&e.key==='?'){show()}else if(e.key==='Escape'){setOpen(false)}}window.addEventListener('keydown',key);return()=>window.removeEventListener('keydown',key)},[open])
 useEffect(()=>{if(open)input.current?.focus();else if(returnFocus.current?.isConnected)returnFocus.current.focus()},[open])
 function choose(page:string){navigate(page);setOpen(false);setQuery('')}
 return <><a className="skip-link" href="#workspace-content">Skip to content</a><div className="workspace-tools"><button className="command-trigger" onClick={show} aria-label="Search workspace and commands"><Search size={15}/><span>Quick actions</span><kbd>Ctrl K</kbd></button><div className="theme-picker" aria-label="Color theme">{[['light',Sun],['dark',Moon],['system',Monitor]].map(([value,Icon])=>{const I=Icon as typeof Sun;return <button key={String(value)} title={`${value} theme`} aria-label={`${value} theme`} aria-pressed={theme===value} onClick={()=>setTheme(String(value))}><I size={16}/></button>})}</div><label className="density-control"><input type="checkbox" checked={compact} onChange={e=>setCompact(e.target.checked)}/>Compact</label></div>{offline&&<div className="offline-status" role="status">You’re offline. Local records remain available; Google and research requests need a connection.</div>}
 {open&&<div className="modal-overlay" onMouseDown={e=>{if(e.target===e.currentTarget)setOpen(false)}}><section className="command-dialog" role="dialog" aria-modal="true" aria-label="Workspace quick actions"><div className="command-search"><Search size={20}/><input ref={input} aria-label="Find a page or action" placeholder="Find a page or action…" value={query} onChange={e=>setQuery(e.target.value)}/><button aria-label="Close quick actions" onClick={()=>setOpen(false)}><X size={18}/></button></div><div className="command-results">{pages.filter(p=>p.toLowerCase().includes(query.toLowerCase())).map(page=><button key={page} onClick={()=>choose(page)}><span>{page}</span><span>Go to page →</span></button>)}{'refresh workspace'.includes(query.toLowerCase())&&<button onClick={()=>{refresh();setOpen(false)}}>Refresh workspace</button>}{!pages.some(p=>p.toLowerCase().includes(query.toLowerCase()))&&!'refresh workspace'.includes(query.toLowerCase())&&<p>No matching actions. Try “inbox” or “companies”.</p>}</div><footer><Keyboard size={14}/> Ctrl/Cmd K: quick actions · Tab: move · Enter: select · Esc: close</footer></section></div>}</>
}

// Focus stays in the foremost dialog, and returns to its trigger after closing.
export function useDialogAccessibility(close:()=>void){
 const closeRef=useRef(close);closeRef.current=close
 useEffect(()=>{let previous:HTMLElement|null=null,dialog:HTMLElement|null=null
 const observer=new MutationObserver(()=>{const next=Array.from(document.querySelectorAll<HTMLElement>('[role="dialog"]')).at(-1)||null;if(next!==dialog){if(next){previous=document.activeElement as HTMLElement;dialog=next;next.querySelector<HTMLElement>('input,textarea,button,a[href]')?.focus()}else{dialog=null;if(previous?.isConnected && previous!==document.body)previous.focus()}}})
 observer.observe(document.body,{childList:true,subtree:true})
 function key(e:KeyboardEvent){if(!dialog)return;if(e.key==='Escape'){e.preventDefault();closeRef.current()}if(e.key==='Tab'){const items=Array.from(dialog.querySelectorAll<HTMLElement>('button:not([disabled]),input:not([disabled]),textarea:not([disabled]),select:not([disabled]),a[href],[tabindex="0"]')).filter(x=>x.getClientRects().length);const first=items[0],last=items.at(-1);if(e.shiftKey&&document.activeElement===first){e.preventDefault();last?.focus()}else if(!e.shiftKey&&document.activeElement===last){e.preventDefault();first?.focus()}}}
 document.addEventListener('keydown',key);return()=>{observer.disconnect();document.removeEventListener('keydown',key)}
 },[])
}
