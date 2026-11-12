import { useState } from 'react'
export function readPreference<T>(key:string, fallback:T):T {
 try { const value=localStorage.getItem('nv:'+key); return value===null?fallback:JSON.parse(value) as T } catch { return fallback }
}
export function usePreference<T>(key:string, fallback:T) {
 const [value,setValue]=useState<T>(()=>readPreference(key,fallback))
 function update(next:T){setValue(next);try{localStorage.setItem('nv:'+key,JSON.stringify(next))}catch{/* Preferences remain usable when storage is blocked. */}}
 return [value,update] as const
}
export function csvText(rows:unknown[][]){return '\ufeff'+rows.map(row=>row.map(value=>{let s=String(value??'');if(/^[=+@\-\t\r]/.test(s))s="'"+s;return '"'+s.replaceAll('"','""')+'"'}).join(',')).join('\r\n')}
export function downloadCsv(name:string,rows:unknown[][]){const url=URL.createObjectURL(new Blob([csvText(rows)],{type:'text/csv;charset=utf-8'}));const a=document.createElement('a');a.href=url;a.download=name;a.click();setTimeout(()=>URL.revokeObjectURL(url),1000)}
