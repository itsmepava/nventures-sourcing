import streamlit as st
import database

def require_login():
    if "user" not in st.session_state:
        st.session_state.user = None
    if st.session_state.user:
        return st.session_state.user

    st.title("nVentures Sourcing")
    st.subheader("Team Login")
    with st.form("login"):
        email = st.text_input("Email")
        password = st.text_input("Password", type="password")
        submitted = st.form_submit_button("Sign in")
    if submitted:
        user = database.authenticate(email, password)
        if user:
            st.session_state.user = user
            st.rerun()
        else:
            st.error("Invalid email or password.")
    st.stop()

def logout():
    if st.sidebar.button("Sign out"):
        st.session_state.user = None
        st.rerun()
