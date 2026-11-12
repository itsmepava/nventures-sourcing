"""Separate inbound pitch funnel and run history."""
from alembic import op
import sqlalchemy as sa
revision = '0003'
down_revision = '0002'
branch_labels = depends_on = None
def upgrade():
    op.create_table('pitches', sa.Column('id', sa.Integer(), primary_key=True), sa.Column('message_id', sa.String(200), unique=True, nullable=False), sa.Column('mode', sa.String(20), nullable=False), sa.Column('name', sa.String(300), nullable=False), sa.Column('status', sa.String(40), nullable=False), sa.Column('data', sa.JSON(), nullable=False), sa.Column('created_at', sa.String(50), nullable=False), sa.Column('error', sa.Text(), nullable=False))
    op.create_table('inbox_jobs', sa.Column('id', sa.Integer(), primary_key=True), sa.Column('status', sa.String(40), nullable=False), sa.Column('data', sa.JSON(), nullable=False), sa.Column('created_at', sa.String(50), nullable=False))
def downgrade():
    op.drop_table('inbox_jobs')
    op.drop_table('pitches')
