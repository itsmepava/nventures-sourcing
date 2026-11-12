import { Component, type ReactNode } from 'react'
export default class ErrorBoundary extends Component<{children:ReactNode},{failed:boolean}> {
 state={failed:false}
 static getDerivedStateFromError(){return {failed:true}}
 render(){return this.state.failed?<main className="recovery-screen"><img src="/brand/logo.png" alt="nVentures"/><h1>Let’s reopen your workspace.</h1><p>The interface encountered a problem. Your saved records remain in the local database.</p><button onClick={()=>window.location.reload()}>Reload workspace</button></main>:this.props.children}
}
