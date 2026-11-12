"""Initial schema: companies, partners, runs, audit events."""
from alembic import op
import sqlalchemy as sa
revision = '0001'
down_revision = None
branch_labels = None
depends_on = None

def upgrade():
    op.create_table('companies',
        sa.Column('id', sa.Integer(), primary_key=True),
        sa.Column('name', sa.String(200), nullable=False),
        sa.Column('name_key', sa.String(200), nullable=False, unique=True),
        sa.Column('domain', sa.String(300), nullable=True, unique=True),
        sa.Column('linkedin_key', sa.String(300), nullable=True, unique=True),
        sa.Column('data', sa.JSON(), nullable=False),
        sa.Column('partner', sa.String(200), nullable=False),
        sa.Column('created_at', sa.String(50), nullable=False),
        sa.Column('sync_status', sa.String(40), nullable=False),
        sa.Column('sheet_row', sa.Integer(), nullable=True),
        sa.Column('sheet_range', sa.String(300), nullable=True),
        sa.Column('sync_error', sa.Text(), nullable=True))
    op.create_table('partners',
        sa.Column('id', sa.Integer(), primary_key=True),
        sa.Column('name', sa.String(200), unique=True, nullable=False),
        sa.Column('website', sa.String(500), nullable=False),
        sa.Column('relevant', sa.Integer(), nullable=False),
        sa.Column('last_sourced', sa.String(50), nullable=False),
        sa.Column('run_count', sa.Integer(), nullable=False),
        sa.Column('last_added', sa.Integer(), nullable=False))
    op.create_table('runs',
        sa.Column('id', sa.Integer(), primary_key=True),
        sa.Column('started_at', sa.String(50), nullable=False),
        sa.Column('finished_at', sa.String(50), nullable=True),
        sa.Column('mode', sa.String(20), nullable=False),
        sa.Column('status', sa.String(30), nullable=False),
        sa.Column('target', sa.Integer(), nullable=False),
        sa.Column('max_partners', sa.Integer(), nullable=False),
        *[sa.Column(key, sa.Integer(), nullable=False) for key in ('accepted', 'rejected', 'duplicates', 'errors', 'researched', 'progress', 'cancel_requested')],
        sa.Column('message', sa.Text(), nullable=False))
    op.create_table('events',
        sa.Column('id', sa.Integer(), primary_key=True),
        sa.Column('run_id', sa.Integer(), sa.ForeignKey('runs.id'), nullable=True),
        sa.Column('created_at', sa.String(50), nullable=False),
        sa.Column('kind', sa.String(40), nullable=False),
        sa.Column('message', sa.Text(), nullable=False),
        sa.Column('detail', sa.JSON(), nullable=False))

def downgrade():
    for name in ('events', 'runs', 'partners', 'companies'):
        op.drop_table(name)
